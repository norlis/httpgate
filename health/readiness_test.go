package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadiness_nilInnerReportsOK(t *testing.T) {
	t.Parallel()
	r := NewReadiness(nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestReadiness_delegatesToInnerWhenReady(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("inner"))
	})
	r := NewReadiness(inner)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418 (inner)", rr.Code)
	}
	if rr.Body.String() != "inner" {
		t.Fatalf("body = %q, want inner", rr.Body.String())
	}
}

func TestReadiness_drainingReports503RegardlessOfInner(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // would be healthy, but draining must win
	})
	r := NewReadiness(inner)
	r.MarkDraining()
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 while draining", rr.Code)
	}
}
