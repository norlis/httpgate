package middleware

import (
	"net/http"

	"github.com/norlis/httpgate/problem"
)

// Shed builds a bulkhead: at most maxInFlight requests execute concurrently;
// any request arriving beyond that is rejected immediately with 503
// problem+json and Retry-After: 1, instead of queueing until the process
// runs out of memory or CPU. This protects the server itself; it is not a
// circuit breaker for upstreams.
//
// Sizing: for CPU-bound work, GOMAXPROCS × (target latency / CPU time per
// request); for I/O-bound work, the pool size of the narrowest dependency
// (e.g. the database's MaxOpenConns). Place it after RequestLogger (so
// rejections are logged) and Recover (so a panic frees the slot through the
// deferred release and is still reported). Panics on maxInFlight <= 0.
func Shed(maxInFlight int) Middleware {
	if maxInFlight <= 0 {
		panic("middleware: Shed requires a positive limit")
	}
	slots := make(chan struct{}, maxInFlight)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case slots <- struct{}{}:
			default:
				w.Header().Set("Retry-After", "1")
				problem.Respond(w, problem.New(
					"service overloaded", http.StatusServiceUnavailable,
					problem.WithDetail("Too many requests in flight; retry shortly."),
					problem.WithInstance(r),
				))
				return
			}
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		})
	}
}
