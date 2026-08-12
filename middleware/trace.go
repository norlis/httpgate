package middleware

import (
	"net/http"

	"github.com/norlis/httpgate/problem"
	"github.com/norlis/httpgate/trace"
)

type traceContextConfig struct {
	responseHeader string
}

// TraceContextOption configures the TraceContext middleware.
type TraceContextOption func(*traceContextConfig)

// WithResponseHeader echoes the trace id to clients in the named response
// header, for correlation from the caller's side. Panics on an empty name:
// that is a programming error, caught at construction.
func WithResponseHeader(name string) TraceContextOption {
	if name == "" {
		panic("middleware: empty trace response header name")
	}
	return func(c *traceContextConfig) { c.responseHeader = http.CanonicalHeaderKey(name) }
}

// TraceContext implements W3C Trace Context ingress (§2.3): a valid incoming
// traceparent keeps its trace id — never regenerated mid-chain — and gets a
// fresh local span id; an absent or malformed one means this service is the
// entrypoint, so both ids are generated. The pair is stored in the request
// context for automatic log injection (package logging) and outbound
// propagation (trace.Transport); the trace id also feeds the problem+json
// requestId. Place it before RequestLogger and Recover in the chain.
func TraceContext(opts ...TraceContextOption) func(http.Handler) http.Handler {
	cfg := traceContextConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tc trace.Context
			if parsed, err := trace.Parse(r.Header.Get(trace.Header)); err == nil {
				tc = trace.Context{TraceID: parsed.TraceID, SpanID: trace.NewSpanID()}
			} else {
				tc = trace.New()
			}

			ctx := trace.NewContext(r.Context(), tc)
			ctx = problem.ContextWithRequestID(ctx, tc.TraceID)
			if cfg.responseHeader != "" {
				w.Header().Set(cfg.responseHeader, tc.TraceID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
