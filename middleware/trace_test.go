package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/norlis/httpgate/problem"
	"github.com/norlis/httpgate/trace"
)

var (
	hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex16 = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// captureTrace runs a request through TraceContext and returns what the inner
// handler observed in its context.
func captureTrace(t *testing.T, mw func(http.Handler) http.Handler, req *http.Request) (trace.Context, string, *httptest.ResponseRecorder) {
	t.Helper()
	var tc trace.Context
	var requestID string
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		tc, _ = trace.FromContext(r.Context())
		requestID = problem.RequestIDFromContext(r.Context())
	})).ServeHTTP(rec, req)
	return tc, requestID, rec
}

func TestTraceContext_InheritsIncomingTraceID(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(trace.Header, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	tc, _, _ := captureTrace(t, TraceContext(), req)

	if tc.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id must be inherited, got %q", tc.TraceID)
	}
	if tc.SpanID == "00f067aa0ba902b7" {
		t.Fatal("span id must be a new local span, not the incoming parent")
	}
	if !hex16.MatchString(tc.SpanID) {
		t.Fatalf("span id %q is not 16 lowercase hex chars", tc.SpanID)
	}
}

func TestTraceContext_GeneratesWhenAbsent(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	tc, _, _ := captureTrace(t, TraceContext(), req)

	if !hex32.MatchString(tc.TraceID) || !hex16.MatchString(tc.SpanID) {
		t.Fatalf("generated ids invalid: %+v", tc)
	}
}

func TestTraceContext_GeneratesWhenMalformed(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(trace.Header, "00-zzzz-boom-01")

	tc, _, _ := captureTrace(t, TraceContext(), req)

	if !hex32.MatchString(tc.TraceID) {
		t.Fatalf("malformed traceparent must yield fresh ids, got %+v", tc)
	}
}

func TestTraceContext_FeedsProblemRequestID(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	tc, requestID, _ := captureTrace(t, TraceContext(), req)

	if requestID != tc.TraceID {
		t.Fatalf("problem requestId %q must equal trace id %q", requestID, tc.TraceID)
	}
}

func TestTraceContext_EchoHeaderOnlyWithOption(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	tc, _, rec := captureTrace(t, TraceContext(WithResponseHeader("X-Trace-Id")), req)
	if got := rec.Header().Get("X-Trace-Id"); got != tc.TraceID {
		t.Fatalf("echoed header = %q, want %q", got, tc.TraceID)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	_, _, rec = captureTrace(t, TraceContext(), req)
	if got := rec.Header().Get("X-Trace-Id"); got != "" {
		t.Fatalf("no option: header must be absent, got %q", got)
	}
}

func TestWithResponseHeader_EmptyNamePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("empty header name must panic (fail fast)")
		}
	}()
	WithResponseHeader("")
}
