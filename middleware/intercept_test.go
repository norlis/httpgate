package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInterceptStatus_writesProblemJSONContentType is the regression test for
// the bug surfaced by examples/basic/test.http: when InterceptStatus rewrites
// a 404/405/500 response, the body is RFC 9457 shape and the Content-Type
// must therefore be "application/problem+json", NOT "application/json".
// Without this, clients negotiating against "application/problem+json"
// would miss the body even though it's exactly what they're asking for.
func TestInterceptStatus_writesProblemJSONContentType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     int
		wantDetail string
	}{
		{
			name:       "404 with custom message",
			status:     http.StatusNotFound,
			wantDetail: "resource not found",
		},
		{
			name:       "405 with custom message",
			status:     http.StatusMethodNotAllowed,
			wantDetail: "method not allowed",
		},
		{
			name:       "500 without custom message uses empty detail",
			status:     http.StatusInternalServerError,
			wantDetail: "",
		},
	}

	mw := InterceptStatus(
		WithIntercept(http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError),
		WithMessage(http.StatusNotFound, "resource not found"),
		WithMessage(http.StatusMethodNotAllowed, "method not allowed"),
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", http.NoBody))

			if rr.Code != tc.status {
				t.Fatalf("status = %d, want %d", rr.Code, tc.status)
			}

			ct := rr.Header().Get("Content-Type")
			want := "application/problem+json; charset=utf-8"
			if ct != want {
				t.Fatalf("Content-Type = %q, want %q", ct, want)
			}

			var body map[string]any
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if int(body["status"].(float64)) != tc.status {
				t.Fatalf("body.status = %v, want %d", body["status"], tc.status)
			}
			if d, _ := body["detail"].(string); d != tc.wantDetail {
				t.Fatalf("body.detail = %q, want %q", d, tc.wantDetail)
			}
		})
	}
}

// TestInterceptStatus_UnwrapSupportsFlush ensures the interceptor exposes the
// underlying writer so streaming handlers (SSE, flush) keep working behind it.
func TestInterceptStatus_UnwrapSupportsFlush(t *testing.T) {
	t.Parallel()
	mw := InterceptStatus(WithIntercept(http.StatusNotFound))
	rec := httptest.NewRecorder()

	var flushErr error
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flushErr = http.NewResponseController(w).Flush()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", http.NoBody))

	if flushErr != nil {
		t.Fatalf("Flush through interceptor: %v", flushErr)
	}
	if !rec.Flushed {
		t.Fatal("underlying recorder was not flushed via the interceptor")
	}
}

// TestInterceptStatus_passesThroughUninterceptedStatuses ensures the
// interceptor does NOT rewrite responses for status codes that weren't
// registered with WithIntercept — preserves bodies + headers untouched.
func TestInterceptStatus_passesThroughUninterceptedStatuses(t *testing.T) {
	t.Parallel()

	mw := InterceptStatus(WithIntercept(http.StatusNotFound))
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("brewing"))
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", http.NoBody))

	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain (pass-through)", ct)
	}
	if rr.Body.String() != "brewing" {
		t.Fatalf("body = %q, want 'brewing'", rr.Body.String())
	}
}

// TestInterceptStatus_dropsHandlerWritesAfterProblemBody is the regression test
// for the response-corruption issue: once an intercepted status has produced
// the problem+json body, any further handler Write must be discarded (not
// appended after the JSON).
func TestInterceptStatus_dropsHandlerWritesAfterProblemBody(t *testing.T) {
	t.Parallel()

	mw := InterceptStatus(
		WithIntercept(http.StatusNotFound),
		WithMessage(http.StatusNotFound, "resource not found"),
	)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		// A handler that keeps writing its own error page after the status.
		n, err := w.Write([]byte("FIRST-LEAK"))
		if err != nil || n != len("FIRST-LEAK") {
			t.Errorf("first write: n=%d err=%v, want full length no error", n, err)
		}
		_, _ = w.Write([]byte("SECOND-LEAK"))
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", http.NoBody))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rr.Body.String()
	if strings.Contains(body, "FIRST-LEAK") || strings.Contains(body, "SECOND-LEAK") {
		t.Fatalf("handler bytes leaked into response body: %q", body)
	}
	// The body must be exactly one valid problem+json document.
	var pd map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &pd); err != nil {
		t.Fatalf("body is not a single valid problem+json: %v (%q)", err, body)
	}
	if pd["detail"] != "resource not found" {
		t.Fatalf("detail = %v, want 'resource not found'", pd["detail"])
	}
}
