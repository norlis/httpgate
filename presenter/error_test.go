package presenter

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/norlis/httpgate/logging"
)

// TestError_5xxDoesNotLeakInternalError is the regression test for the
// info-leak: a 500 must never surface the raw err.Error() to the client.
func TestError_5xxDoesNotLeakInternalError(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", http.NoBody)
	Error(rr, req, errors.New("dial tcp 10.0.0.1:5432: connection refused"))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	var pd map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&pd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pd["status"].(float64) != http.StatusInternalServerError {
		t.Fatalf("payload status = %v, want 500", pd["status"])
	}
	if d, ok := pd["detail"]; ok {
		t.Fatalf("5xx leaked detail to client: %v", d)
	}
	if pd["title"] != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("title = %v, want %q", pd["title"], http.StatusText(http.StatusInternalServerError))
	}
}

// TestError_5xxLogsRealErrorButHidesIt verifies the real error reaches the
// logger while staying out of the response body.
func TestError_5xxLogsRealErrorButHidesIt(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", http.NoBody)

	Error(rr, req, errors.New("secret-dsn-leak"), WithLogger(logger))

	if !strings.Contains(buf.String(), "secret-dsn-leak") {
		t.Fatalf("logger did not record the real error: %q", buf.String())
	}
	if strings.Contains(rr.Body.String(), "secret-dsn-leak") {
		t.Fatalf("response body leaked the real error: %q", rr.Body.String())
	}
}

func TestError_appliesStatusAndTitle(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", http.NoBody)
	Error(
		rr, req, errors.New("bad input"),
		WithStatus(http.StatusBadRequest),
		WithTitle("validation failed"),
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	var pd map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&pd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pd["title"] != "validation failed" {
		t.Fatalf("title = %v, want validation failed", pd["title"])
	}
	// 4xx are client errors: the message is safe and useful to surface.
	if pd["detail"] != "bad input" {
		t.Fatalf("detail = %v, want bad input (4xx should surface the error)", pd["detail"])
	}
}

func TestError_LogsStructuredServerError(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	log := logging.New(buf)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/payments", nil)
	Error(rec, req, errors.New("db down"), WithLogger(log))

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, buf.String())
	}
	if m[logging.KeyMessage] != "server error" {
		t.Fatalf("message = %v", m[logging.KeyMessage])
	}
	if m[logging.KeyErrorType] != "*errors.errorString" {
		t.Fatalf("error.type = %v", m[logging.KeyErrorType])
	}
	if m[logging.KeyErrorMessage] != "db down" {
		t.Fatalf("error.message = %v", m[logging.KeyErrorMessage])
	}
	if m[logging.KeyHTTPResponseStatusCode] != float64(http.StatusInternalServerError) {
		t.Fatalf("http.response.status_code = %v", m[logging.KeyHTTPResponseStatusCode])
	}
}
