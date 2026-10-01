package problem

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Pins the exact bytes Respond writes. Expectations change ONLY with a
// documented wire-format decision (see CHANGELOG).
func TestRespond_wireBytes(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	d := &Detail{Title: "Not Found", Status: 404, Detail: "a <b> & c", Timestamp: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	Respond(rr, d)
	want := `{"title":"Not Found","status":404,"detail":"a <b> & c","timestamp":"2026-10-01T12:00:00Z"}`
	if got := rr.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestRespond_minimalDetailOmitsOptionals(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	Respond(rr, &Detail{Title: "Teapot", Status: 418})
	want := `{"title":"Teapot","status":418}`
	if got := rr.Body.String(); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}
