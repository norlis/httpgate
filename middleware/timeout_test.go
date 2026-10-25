package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTimeout_responds504WhenHandlerHonorsDeadline(t *testing.T) {
	t.Parallel()
	h := Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // a well-behaved handler returns on deadline without writing
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/slow", http.NoBody))

	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var pd map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&pd); err != nil || pd["instance"] != "/slow" {
		t.Fatalf("problem body = %v (err %v)", pd, err)
	}
}

func TestTimeout_doesNotOverwriteCommittedResponse(t *testing.T) {
	t.Parallel()
	h := Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		<-r.Context().Done()
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/stream", http.NoBody))

	if rr.Code != http.StatusOK || rr.Body.String() != "partial" {
		t.Fatalf("committed response was altered: %d %q", rr.Code, rr.Body.String())
	}
}

func TestTimeout_fastHandlerUntouched(t *testing.T) {
	t.Parallel()
	h := Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("handler context has no deadline")
		}
		w.WriteHeader(http.StatusCreated)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", http.NoBody))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rr.Code)
	}
}

func TestTimeout_clientCancelIsNot504(t *testing.T) {
	t.Parallel()
	h := Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/", http.NoBody).WithContext(ctx)
	rr := httptest.NewRecorder()
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusGatewayTimeout {
		t.Fatal("client cancellation must not be reported as a server-side 504")
	}
}

func TestTimeout_panicsOnNonPositiveDuration(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("Timeout(0) must panic at construction")
		}
	}()
	_ = Timeout(0)
}

func TestTimeout_dropsStaleEntityHeadersOn504(t *testing.T) {
	t.Parallel()
	h := Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("ETag", `"abc"`)
		<-r.Context().Done()
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/slow", http.NoBody))
	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rr.Code)
	}
	for _, k := range []string{"Content-Length", "Content-Encoding", "ETag"} {
		if v := rr.Header().Get(k); v != "" {
			t.Errorf("%s = %q, must be dropped before the problem+json body", k, v)
		}
	}
}
