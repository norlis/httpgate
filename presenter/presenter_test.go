package presenter

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSON_writesDefaults(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	JSON(rr, req, map[string]string{"status": "ok"})

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "ok" {
		t.Fatalf("body status = %q, want ok", got["status"])
	}
}

func TestJSON_appliesOptions(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	JSON(
		rr, req, map[string]int{"n": 1},
		WithStatusCode(http.StatusAccepted),
		WithHeader("X-Test", "1"),
	)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	if v := rr.Header().Get("X-Test"); v != "1" {
		t.Fatalf("X-Test header = %q, want 1", v)
	}
}

func TestPlainText_writesPlainBody(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", http.NoBody)
	PlainText(rr, req, "hello")
	body, _ := io.ReadAll(rr.Body)
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain*", ct)
	}
}
