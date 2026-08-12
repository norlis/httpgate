package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/norlis/httpgate/logging"
	"github.com/norlis/httpgate/presenter"
)

// Recover builds middleware that recovers from panics, logs the panic once as
// the standard structured error object, and responds 500 as RFC 9457 (except
// for http.ErrAbortHandler, which is re-panicked, and Upgrade connections,
// which are left untouched).
func Recover(log *slog.Logger) func(next http.Handler) http.Handler {
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
					log.ErrorContext(
						r.Context(),
						"panic recovered",
						slog.String(logging.KeyErrorType, "panic"),
						slog.String(logging.KeyErrorMessage, fmt.Sprint(rvr)),
						slog.String(logging.KeyErrorStackTrace, string(debug.Stack())),
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
