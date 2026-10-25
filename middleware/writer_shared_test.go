package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/norlis/httpgate/logging"
)

// Characterization tests for the ResponseWriter wrapper shared across
// InterceptStatus, RequestLogger and Timeout (proposal C2).

func interceptNotFound() Middleware {
	return InterceptStatus(WithIntercept(http.StatusNotFound), WithMessage(http.StatusNotFound, "nope"))
}

func decodeProblem(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var pd map[string]any
	if err := json.Unmarshal(body, &pd); err != nil {
		t.Fatalf("body is not a single problem document: %v (%q)", err, body)
	}
	return pd
}

// WrapWriter must not stack wrappers: the second call returns the first one so
// every middleware in the chain observes the same status/bytes/committed state.
func TestWrapWriter_IsIdempotent(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	first := WrapWriter(rec)
	second := WrapWriter(first)
	if first != second {
		t.Fatal("WrapWriter(WrapWriter(w)) must return the existing wrapper")
	}
	second.WriteHeader(http.StatusTeapot)
	if first.Status() != http.StatusTeapot {
		t.Fatalf("status not shared: %d", first.Status())
	}
}

// B3: io.Copy from a handler behind InterceptStatus must still reach the
// underlying ReadFrom (sendfile) when nothing was intercepted.
func TestInterceptStatus_ReadFromReachesUnderlying(t *testing.T) {
	t.Parallel()
	rec := &readFromRecorder{ResponseRecorder: httptest.NewRecorder()}
	payload := strings.Repeat("y", 4096)
	h := interceptNotFound()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.Copy(w, struct{ io.Reader }{strings.NewReader(payload)})
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/f", http.NoBody))
	if !rec.viaReadFrom {
		t.Fatal("io.Copy did not reach the underlying ReadFrom through InterceptStatus")
	}
	if rec.Body.String() != payload {
		t.Fatalf("body length = %d, want %d", rec.Body.Len(), len(payload))
	}
}

// After an intercepted status, a handler that streams its own error page via
// io.Copy must not leak it after the problem document.
func TestInterceptStatus_ReadFromDroppedAfterIntercept(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	h := interceptNotFound()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		n, err := io.Copy(w, struct{ io.Reader }{strings.NewReader("LEAK-PAGE")})
		if err != nil || n != int64(len("LEAK-PAGE")) {
			t.Errorf("io.Copy = %d, %v; handler must see a full copy", n, err)
		}
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", http.NoBody))
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "LEAK") {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	decodeProblem(t, rec.Body.Bytes())
}

// B4: the access line's body size is the problem document actually sent, not
// the bytes the handler tried to write (chain order Intercept → Logger).
func TestRequestLogger_BodySizeIsTheInterceptedDocument(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	h := New(interceptNotFound(), RequestLogger(log)).Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("handler page that is longer than anything"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", http.NoBody))

	line := lastLine(t, buf)
	if got := line[logging.KeyHTTPResponseStatusCode]; got != float64(http.StatusNotFound) {
		t.Fatalf("status_code = %v, want 404", got)
	}
	if got := line[logging.KeyHTTPResponseBodySize]; got != float64(rec.Body.Len()) {
		t.Fatalf("body.size = %v, want %d (bytes on the wire)", got, rec.Body.Len())
	}
}

// B5: once a status is intercepted, a later WriteHeader must not commit a
// different status on the real writer ahead of the problem document.
func TestInterceptStatus_SecondWriteHeaderDoesNotCommit(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	h := interceptNotFound()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.WriteHeader(http.StatusOK) // handler changes its mind: first status wins, as in net/http
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	decodeProblem(t, rec.Body.Bytes())
}

// Timeout behind InterceptStatus: an intercepted status counts as committed, so
// the 504 is never written on top of the pending 404.
func TestTimeout_InterceptedStatusCountsAsCommitted(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	h := New(interceptNotFound(), Timeout(20*time.Millisecond)).Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		<-r.Context().Done()
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (no 504 over an intercepted status)", rec.Code)
	}
	pd := decodeProblem(t, rec.Body.Bytes())
	if pd["detail"] != "nope" {
		t.Fatalf("detail = %v", pd["detail"])
	}
}

// Full recommended stack: Intercept → Logger → Timeout. Status and bytes must be
// consistent across all three, and a plain 200 passes through untouched.
func TestWriterStack_PlainResponseIsConsistent(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	h := New(interceptNotFound(), RequestLogger(log), Timeout(time.Second)).Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", http.NoBody))
	if rec.Code != http.StatusCreated || rec.Body.String() != "hello" || rec.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("response altered: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	line := lastLine(t, buf)
	if line[logging.KeyHTTPResponseStatusCode] != float64(201) || line[logging.KeyHTTPResponseBodySize] != float64(5) {
		t.Fatalf("access line = %v", line)
	}
}

// Two InterceptStatus instances in one chain stay independent: the inner one
// rewrites its codes, the outer one sees the already-committed result.
func TestInterceptStatus_NestedInstancesAreIndependent(t *testing.T) {
	t.Parallel()
	outer := InterceptStatus(WithIntercept(http.StatusInternalServerError), WithMessage(http.StatusInternalServerError, "outer"))
	inner := InterceptStatus(WithIntercept(http.StatusNotFound), WithMessage(http.StatusNotFound, "inner"))

	for _, tc := range []struct {
		status int
		detail string
	}{{http.StatusNotFound, "inner"}, {http.StatusInternalServerError, "outer"}} {
		rec := httptest.NewRecorder()
		h := New(outer, inner).Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", http.NoBody))
		if rec.Code != tc.status {
			t.Fatalf("status = %d, want %d", rec.Code, tc.status)
		}
		if pd := decodeProblem(t, rec.Body.Bytes()); pd["detail"] != tc.detail {
			t.Fatalf("detail = %v, want %q", pd["detail"], tc.detail)
		}
	}
}

// BenchmarkWriterStack measures the per-request cost of the three
// writer-wrapping middlewares composed in the recommended order.
func BenchmarkWriterStack(b *testing.B) {
	log := slog.New(slog.DiscardHandler)
	h := New(interceptNotFound(), RequestLogger(log), Timeout(time.Second)).Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest("GET", "/", http.NoBody)
	rr := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		rr.Body.Reset()
		h.ServeHTTP(rr, req)
	}
}
