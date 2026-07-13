package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestLogger returns a logger writing JSON to buf so tests can count the
// emitted "request" lines.
func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, nil)), buf
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
	return strings.Count(buf.String(), `"msg":"request"`)
}

func serve(t *testing.T, mw func(http.Handler) http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	mw(statusOKHandler()).ServeHTTP(rec, req)
	return rec
}

func TestRequestLogger_SkipsListedPath(t *testing.T) {
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
	log, buf := newTestLogger()
	serve(t, RequestLogger(log, WithSkipPaths("/health")), "/api/entry")

	if n := requestLines(buf); n != 1 {
		t.Fatalf("unlisted path: want 1 request line, got %d", n)
	}
	if body := buf.String(); !strings.Contains(body, `"method":"GET"`) {
		t.Fatalf("access line missing method attr: %s", body)
	}
}

func TestRequestLogger_NoOptionLogsEverything(t *testing.T) {
	log, buf := newTestLogger()
	serve(t, RequestLogger(log), "/health")

	if n := requestLines(buf); n != 1 {
		t.Fatalf("no option: want 1 request line, got %d", n)
	}
}

func TestRequestLogger_ExactMatchIgnoresQuery(t *testing.T) {
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

func TestWithSkipPaths_Additive(t *testing.T) {
	log, buf := newTestLogger()
	mw := RequestLogger(log, WithSkipPaths("/a"), WithSkipPaths("/b"))

	serve(t, mw, "/a")
	serve(t, mw, "/b")
	if n := requestLines(buf); n != 0 {
		t.Fatalf("both paths should be skipped: want 0, got %d", n)
	}
}
