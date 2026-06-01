package problem

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNew_marshalsRFC9457Shape(t *testing.T) {
	t.Parallel()
	d := New(
		"forbidden", http.StatusForbidden,
		WithDetail("missing scope"),
		WithType("https://example.com/probs/forbidden"),
	)
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	for _, k := range []string{"title", "status", "detail", "type", "timestamp"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing required field %q in %v", k, got)
		}
	}
	if got["status"].(float64) != http.StatusForbidden {
		t.Errorf("status = %v, want 403", got["status"])
	}
}

func TestFromError_usesErrorMessageAsDetail(t *testing.T) {
	t.Parallel()
	d := FromError(errors.New("db down"), http.StatusServiceUnavailable)
	if d.Title != http.StatusText(http.StatusServiceUnavailable) {
		t.Errorf("title = %q", d.Title)
	}
	if d.Detail != "db down" {
		t.Errorf("detail = %q, want 'db down'", d.Detail)
	}
}

func TestRespond_setsContentTypeAndStatus(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	Respond(rr, New("teapot", http.StatusTeapot))
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestWithInstance_setsPathAndRequestIDFromContext(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/api/users/42", http.NoBody)
	req = req.WithContext(ContextWithRequestID(req.Context(), "abc-123"))
	d := New("nope", http.StatusBadRequest, WithInstance(req))
	if d.Instance != "/api/users/42" {
		t.Fatalf("instance = %q", d.Instance)
	}
	if d.RequestID != "abc-123" {
		t.Fatalf("requestID = %q", d.RequestID)
	}
}

func TestWithInstance_noRequestIDWhenContextEmpty(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/x", http.NoBody)
	d := New("nope", http.StatusBadRequest, WithInstance(req))
	if d.RequestID != "" {
		t.Fatalf("requestID = %q, want empty", d.RequestID)
	}
}

func TestRequestIDFromContext_roundTrips(t *testing.T) {
	t.Parallel()
	ctx := ContextWithRequestID(context.Background(), "xyz-789")
	if got := RequestIDFromContext(ctx); got != "xyz-789" {
		t.Fatalf("got %q, want xyz-789", got)
	}
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Fatalf("empty context: got %q, want \"\"", got)
	}
}

func TestNew_timestampIsRecentUTC(t *testing.T) {
	t.Parallel()
	before := time.Now().UTC()
	d := New("x", 500)
	after := time.Now().UTC()
	if d.Timestamp.Before(before) || d.Timestamp.After(after) {
		t.Fatalf("timestamp %v not in [%v, %v]", d.Timestamp, before, after)
	}
	if d.Timestamp.Location() != time.UTC {
		t.Fatalf("timestamp not UTC: %v", d.Timestamp.Location())
	}
}

func TestDetail_zeroTimestampOmitted(t *testing.T) {
	t.Parallel()
	d := &Detail{Title: "x", Status: 500} // Timestamp left as zero
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "timestamp") {
		t.Fatalf("zero timestamp should be omitted: %s", b)
	}
}
