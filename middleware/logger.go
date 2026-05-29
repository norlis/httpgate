package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// RequestLogger builds middleware that logs one structured "request" line per
// request, including status, duration, URI, method, remote address and bytes.
func RequestLogger(log *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
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
		}
		return http.HandlerFunc(fn)
	}
}
