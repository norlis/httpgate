package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/norlis/httpgate/logging"
)

func TestRecover_LogsPanicAsStructuredError(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()

	rec := httptest.NewRecorder()
	Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, buf.String())
	}
	if m[logging.KeyMessage] != "panic recovered" {
		t.Fatalf("message = %v", m[logging.KeyMessage])
	}
	if m[logging.KeyLevel] != "error" {
		t.Fatalf("log.level = %v", m[logging.KeyLevel])
	}
	if m[logging.KeyErrorType] != "panic" {
		t.Fatalf("error.type = %v", m[logging.KeyErrorType])
	}
	if m[logging.KeyErrorMessage] != "boom" {
		t.Fatalf("error.message = %v", m[logging.KeyErrorMessage])
	}
	stack, _ := m[logging.KeyErrorStackTrace].(string)
	if !strings.Contains(stack, "goroutine") {
		t.Fatalf("error.stack_trace missing: %v", m)
	}
	if _, ok := m["stacktrace"]; ok {
		t.Fatalf("ad-hoc stacktrace field must be gone: %v", m)
	}
}

// B9: a panic after the response was committed must be logged but must not
// write a second status or append a problem document to the partial body.
func TestRecover_DoesNotOverwriteCommittedResponse(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	rec := httptest.NewRecorder()
	h := Recover(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("boom after commit")
	}))
	h.ServeHTTP(WrapWriter(rec), httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "partial" {
		t.Fatalf("committed response altered: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(buf.String(), `"message":"panic recovered"`) {
		t.Fatalf("panic must still be logged: %s", buf.String())
	}
}

func TestRecover_UncommittedPanicStillAnswers500ThroughWrapper(t *testing.T) {
	t.Parallel()
	log, _ := newTestLogger()
	rec := httptest.NewRecorder()
	h := Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	h.ServeHTTP(WrapWriter(rec), httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("status=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// Characterization before extracting the defer body: http.ErrAbortHandler is
// the server's own sentinel and must leave Recover untouched, not logged.
func TestRecover_RePanicsErrAbortHandler(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	h := Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		rvr := recover()
		err, ok := rvr.(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("recovered %v, want http.ErrAbortHandler re-panicked", rvr)
		}
		if buf.Len() != 0 {
			t.Fatalf("ErrAbortHandler must not be logged: %s", buf.String())
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("handler returned normally; the panic was swallowed")
}

// Characterization: on an Upgrade connection the panic is logged but no
// problem document is written, since the client no longer speaks HTTP/1.1.
func TestRecover_UpgradeConnectionIsLoggedButNotAnswered(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")

	Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})).ServeHTTP(rec, req)

	if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Fatalf("body/headers written on Upgrade: %q %v", rec.Body.String(), rec.Header())
	}
	if !strings.Contains(buf.String(), "panic recovered") {
		t.Fatalf("panic not logged: %s", buf.String())
	}
}
