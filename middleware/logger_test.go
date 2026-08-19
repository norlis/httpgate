package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/norlis/httpgate/logging"
)

// newTestLogger returns a standard platform logger writing to buf so tests
// assert the exact emitted contract.
func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return logging.New(buf), buf
}

// okHandler is defined in chain_test.go but we need one that writes StatusOK.
func statusOKHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

// requestLines counts emitted access-log lines in buf.
func requestLines(buf *bytes.Buffer) int {
	return strings.Count(buf.String(), `"message":"request completed"`)
}

func serve(t *testing.T, mw func(http.Handler) http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	mw(statusOKHandler()).ServeHTTP(rec, req)
	return rec
}

func TestRequestLogger_SkipsListedPath(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	rec := serve(t, RequestLogger(log, WithSkipPaths("/health")), "/health")

	if n := requestLines(buf); n != 0 {
		t.Fatalf("skipped path: want 0 request lines, got %d", n)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("next must still run: status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRequestLogger_LogsUnlistedPath(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	serve(t, RequestLogger(log, WithSkipPaths("/health")), "/api/entry")

	if n := requestLines(buf); n != 1 {
		t.Fatalf("unlisted path: want 1 request line, got %d", n)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil {
		t.Fatalf("access line is not valid JSON: %v", err)
	}
	if m[logging.KeyHTTPRequestMethod] != "GET" {
		t.Fatalf("missing %s: %v", logging.KeyHTTPRequestMethod, m)
	}
	if m[logging.KeyURLPath] != "/api/entry" {
		t.Fatalf("missing %s: %v", logging.KeyURLPath, m)
	}
	if m[logging.KeyHTTPResponseStatusCode] != float64(http.StatusOK) {
		t.Fatalf("missing %s: %v", logging.KeyHTTPResponseStatusCode, m)
	}
	d, ok := m[logging.KeyEventDuration].(float64)
	if !ok || d <= 0 {
		t.Fatalf("%s must be a positive number of nanoseconds: %v", logging.KeyEventDuration, m)
	}
	if want := time.Duration(int64(d)).String(); m[logging.KeyEventDurationHuman] != want {
		t.Fatalf("%s = %v, want %q (mirror of %s): %v", logging.KeyEventDurationHuman, m[logging.KeyEventDurationHuman], want, logging.KeyEventDuration, m)
	}
	if _, ok := m["logger"]; ok {
		t.Fatalf("ad-hoc logger field must be gone: %v", m)
	}
}

func TestRequestLogger_NoOptionLogsEverything(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	serve(t, RequestLogger(log), "/health")

	if n := requestLines(buf); n != 1 {
		t.Fatalf("no option: want 1 request line, got %d", n)
	}
}

func TestRequestLogger_ExactMatchIgnoresQuery(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	mw := RequestLogger(log, WithSkipPaths("/health"))

	serve(t, mw, "/health?probe=1") // same path, query ignored -> skipped
	if n := requestLines(buf); n != 0 {
		t.Fatalf("query should not affect match: want 0, got %d", n)
	}

	serve(t, mw, "/healthz") // prefix, not exact -> logged
	if n := requestLines(buf); n != 1 {
		t.Fatalf("/healthz must be logged (exact match): want 1, got %d", n)
	}
}

// TestRequestLogger_CarriesTraceContext pins the branch's headline guarantee:
// when TraceContext runs before RequestLogger, the access line carries
// trace_id/span_id inherited from the incoming traceparent.
func TestRequestLogger_CarriesTraceContext(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	chain := New(TraceContext(), RequestLogger(log))

	req := httptest.NewRequest(http.MethodGet, "/api/entry", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	chain.Then(statusOKHandler()).ServeHTTP(rec, req)

	m := lastLine(t, buf)
	if m[logging.KeyTraceID] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("%s = %v, want inherited trace id", logging.KeyTraceID, m[logging.KeyTraceID])
	}
	spanID, _ := m[logging.KeySpanID].(string)
	if spanID == "" || spanID == "00f067aa0ba902b7" {
		t.Fatalf("%s = %q, want a fresh non-empty local span id", logging.KeySpanID, spanID)
	}
}

// TestRequestLogger_WrapsRecoverCapturesRecoveredStatus pins finding #3's fix:
// with the correct chain order (RequestLogger wrapping Recover), a panicking
// handler's recovered 500 status is captured by the access line.
func TestRequestLogger_WrapsRecoverCapturesRecoveredStatus(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	chain := New(RequestLogger(log), Recover(log))

	panicking := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/entry", nil)
	rec := httptest.NewRecorder()
	chain.Then(panicking).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want 500", rec.Code)
	}

	var found bool
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("log line is not valid JSON: %v\n%s", err, line)
		}
		if m[logging.KeyMessage] != "request completed" {
			continue
		}
		found = true
		if m[logging.KeyHTTPResponseStatusCode] != float64(http.StatusInternalServerError) {
			t.Fatalf("%s = %v, want 500", logging.KeyHTTPResponseStatusCode, m[logging.KeyHTTPResponseStatusCode])
		}
	}
	if !found {
		t.Fatalf("no \"request completed\" access line found: %s", buf.String())
	}
}

// lastLine decodes the last emitted NDJSON line of buf.
func lastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var m map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &m); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, buf.String())
	}
	return m
}

func TestWithSkipPaths_Additive(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	mw := RequestLogger(log, WithSkipPaths("/a"), WithSkipPaths("/b"))

	serve(t, mw, "/a")
	serve(t, mw, "/b")
	if n := requestLines(buf); n != 0 {
		t.Fatalf("both paths should be skipped: want 0, got %d", n)
	}
}
