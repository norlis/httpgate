package middleware

import (
	"encoding/json"
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
