package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/norlis/httpgate/problem"
)

// Timeout builds middleware that gives each request a deadline of d via its
// context and, when the handler returns because that deadline expired without
// having committed a response, answers 504 as RFC 9457 problem+json.
//
// The handler runs in the request goroutine (no buffering, no extra
// goroutine), so streaming and SSE keep working and a response that was
// already committed is never altered. The trade-off: a handler that ignores
// ctx is not interrupted — the same contract as Spring's request-timeout.
// Keep d below server.New's WriteTimeout. Panics on d <= 0.
func Timeout(d time.Duration) Middleware {
	if d <= 0 {
		panic("middleware: Timeout requires a positive duration")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			ww := WrapWriter(w).(*responseWriter)
			next.ServeHTTP(ww, r.WithContext(ctx))

			if ww.wroteHeader || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return
			}
			dropEntityHeaders(w.Header())
			problem.Respond(w, problem.New(
				"request timed out", http.StatusGatewayTimeout,
				problem.WithDetail("The server did not complete the request within the allowed time."),
				problem.WithInstance(r),
			))
		})
	}
}
