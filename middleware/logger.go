package middleware

import (
	"log/slog"
	"net/http"
	"time"
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

// RequestLogger builds middleware that logs one structured "request" line per
// request, including status, duration, URI, method, remote address and bytes.
// Paths registered via WithSkipPaths are served without a log line.
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
				log.With(slog.String("logger", "middleware")).
					Info(
						"request",
						slog.Group(
							"http",
							slog.Int("status", ww.Status()),
							slog.String("duration", time.Since(t0).String()),
							slog.String("uri", r.RequestURI),
							slog.String("method", r.Method),
							slog.String("remoteAddr", r.RemoteAddr),
							slog.Int("bytes", ww.BytesWritten()),
						),
					)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
