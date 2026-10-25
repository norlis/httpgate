package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWrapWriter_UnwrapExposesFlush ensures the wrapper does not hide the
// underlying writer's capabilities: http.NewResponseController must reach
// Flush through Unwrap (Go 1.20+). Without it, SSE/streaming break silently.
func TestWrapWriter_UnwrapExposesFlush(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	ww := WrapWriter(rec)

	if err := http.NewResponseController(ww).Flush(); err != nil {
		t.Fatalf("Flush through wrapper: %v", err)
	}
	if !rec.Flushed {
		t.Fatal("underlying recorder was not flushed via the wrapper")
	}
}

func TestWrapWriter_UnwrapReturnsUnderlying(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	ww := WrapWriter(rec)

	u, ok := ww.(interface{ Unwrap() http.ResponseWriter })
	if !ok {
		t.Fatal("WrapWriter result does not implement Unwrap")
	}
	if u.Unwrap() != rec {
		t.Fatal("Unwrap did not return the underlying writer")
	}
}

// flushRecorder records direct (non-ResponseController) Flush calls, the way
// SSE libraries that type-assert w.(http.Flusher) observe them.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed int
}

func (f *flushRecorder) Flush() { f.flushed++; f.ResponseRecorder.Flush() }

func TestWrapWriter_ImplementsFlusherDirectly(t *testing.T) {
	t.Parallel()
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	ww := WrapWriter(rec)
	f, ok := ww.(http.Flusher)
	if !ok {
		t.Fatal("WrapWriter result must implement http.Flusher: SSE code type-asserts it directly")
	}
	f.Flush()
	if rec.flushed != 1 {
		t.Fatalf("underlying Flush called %d times, want 1", rec.flushed)
	}
}

// readFromRecorder reports whether io.Copy reached it through ReadFrom, which
// is what net/http needs to use sendfile for static content.
type readFromRecorder struct {
	*httptest.ResponseRecorder
	viaReadFrom bool
}

func (r *readFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.viaReadFrom = true
	return io.Copy(r.ResponseRecorder, src)
}

func TestWrapWriter_ReadFromCountsBytes(t *testing.T) {
	t.Parallel()
	rec := &readFromRecorder{ResponseRecorder: httptest.NewRecorder()}
	ww := WrapWriter(rec)
	payload := strings.Repeat("x", 70_000)

	// io.Copy prefers src.WriteTo over dst.ReadFrom; a plain reader (like a
	// file through net/http) forces the ReadFrom path we want to verify.
	n, err := io.Copy(ww, struct{ io.Reader }{strings.NewReader(payload)})
	if err != nil || n != int64(len(payload)) {
		t.Fatalf("io.Copy = %d, %v", n, err)
	}
	if !rec.viaReadFrom {
		t.Fatal("io.Copy did not reach the underlying ReadFrom: sendfile optimization lost")
	}
	if ww.BytesWritten() != len(payload) {
		t.Fatalf("BytesWritten = %d, want %d", ww.BytesWritten(), len(payload))
	}
	if ww.Status() != http.StatusOK {
		t.Fatalf("Status = %d, want 200", ww.Status())
	}
}

func TestWrapWriter_ReadFromFallsBackWhenUnsupported(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder() // *ResponseRecorder has no ReadFrom
	ww := WrapWriter(rec)
	n, err := io.Copy(ww, bytes.NewReader([]byte("hello")))
	if err != nil || n != 5 || ww.BytesWritten() != 5 || rec.Body.String() != "hello" {
		t.Fatalf("fallback copy: n=%d err=%v bytes=%d body=%q", n, err, ww.BytesWritten(), rec.Body.String())
	}
}

// unwrapOnlyFlusher flushes only through Unwrap, the idiom Go recommends since
// 1.20 (http.ResponseController); it does not implement http.Flusher itself.
type unwrapOnlyFlusher struct {
	http.ResponseWriter
	inner *flushRecorder
}

func (u unwrapOnlyFlusher) Unwrap() http.ResponseWriter { return u.inner }

func TestWrapWriter_FlushReachesUnwrapOnlyWriter(t *testing.T) {
	t.Parallel()
	inner := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	ww := WrapWriter(unwrapOnlyFlusher{ResponseWriter: inner, inner: inner})
	if err := http.NewResponseController(ww).Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if inner.flushed != 1 {
		t.Fatalf("underlying Flush called %d times, want 1: direct type assertion skips Unwrap-only writers", inner.flushed)
	}
}

func TestWrapWriter_InformationalStatusIsNotFinal(t *testing.T) {
	t.Parallel()
	ww := WrapWriter(httptest.NewRecorder())
	ww.WriteHeader(http.StatusEarlyHints)
	ww.WriteHeader(http.StatusOK)
	if ww.Status() != http.StatusOK {
		t.Fatalf("Status = %d after 103 then 200, want 200", ww.Status())
	}
}
