package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/norlis/httpgate/presenter"
)

// Recover builds middleware that recovers from panics, logs the stack trace,
// and responds 500 as RFC 9457 (except for http.ErrAbortHandler, which is
// re-panicked, and Upgrade connections, which are left untouched).
func Recover(log *slog.Logger) func(next http.Handler) http.Handler {
	logger := log.With(slog.String("logger", "middleware.recover"))
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					// Let the server handle its own abort sentinel; re-panic it
					// untouched, exactly as net/http does internally. rvr is a
					// recovered value (any), so assert to error before matching.
					if err, ok := rvr.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(rvr)
					}
					logger.Error(
						"recovered from panic",
						slog.String("stacktrace", string(debug.Stack())),
					)
					if r.Header.Get("Connection") != "Upgrade" {
						presenter.Error(
							w, r,
							errors.New("recovered from panic"),
							presenter.WithStatus(http.StatusInternalServerError),
						)
					}
				}
			}()
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	}
}
