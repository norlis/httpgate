package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/norlis/httpgate/logging"
)

type requestLoggerConfig struct {
	skip map[string]struct{} // nil ⇒ log every path (zero overhead).
}

// RequestLoggerOption configures RequestLogger.
type RequestLoggerOption func(*requestLoggerConfig)

// WithSkipPaths suppresses the access line for the given exact URL.Path values
// (query string ignored). Additive across calls.
func WithSkipPaths(paths ...string) RequestLoggerOption {
	return func(c *requestLoggerConfig) {
		if len(paths) == 0 {
			return
		}
		if c.skip == nil {
			c.skip = make(map[string]struct{}, len(paths))
		}
		for _, p := range paths {
			c.skip[p] = struct{}{}
		}
	}
}

// RequestLogger builds middleware that logs one "request completed" line per
// request with the standard fields: http.request.method, url.path,
// http.response.status_code, http.response.body.size, client.address and
// event.duration (nanoseconds). Run TraceContext earlier in the chain so the
// line carries trace_id/span_id. Paths registered via WithSkipPaths are
// served without a log line.
//
// Chain order matters: RequestLogger must come after TraceContext (to carry
// trace_id/span_id) and BEFORE Recover — i.e. RequestLogger must wrap
// Recover, not the other way around — so that a panic's recovered 500 status
// is captured in http.response.status_code instead of being written to the
// outer ResponseWriter after the access line's status has already been read.
// Caveat: client.address is r.RemoteAddr verbatim, so it includes the port,
// and behind a load balancer or reverse proxy it is the proxy's address, not
// the original client's — parse X-Forwarded-For yourself if you need that.
func RequestLogger(log *slog.Logger, opts ...RequestLoggerOption) func(next http.Handler) http.Handler {
	cfg := &requestLoggerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := cfg.skip[r.URL.Path]; skip {
				next.ServeHTTP(w, r) // skip wrap/timing for hot probe paths.
				return
			}

			ww := WrapWriter(w)
			t0 := time.Now()
			defer func() {
				log.InfoContext(
					r.Context(),
					"request completed",
					slog.String(logging.KeyHTTPRequestMethod, r.Method),
					slog.String(logging.KeyURLPath, r.URL.Path),
					slog.Int(logging.KeyHTTPResponseStatusCode, ww.Status()),
					slog.Int(logging.KeyHTTPResponseBodySize, ww.BytesWritten()),
					slog.String(logging.KeyClientAddress, r.RemoteAddr),
					slog.Int64(logging.KeyEventDuration, time.Since(t0).Nanoseconds()),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
