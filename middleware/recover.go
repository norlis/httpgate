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
// which are left untouched). When the response was already committed through
// this package's writer wrapper (RequestLogger, Timeout or InterceptStatus
// earlier in the chain), nothing more is written: a second status would be
// superfluous and the problem document would corrupt the partial body.
func Recover(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					handlePanic(log, w, r, rvr)
				}
			}()
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	}
}

// handlePanic is the recovery path of Recover, kept out of the defer so the
// decision tree stays readable: re-panic the server's abort sentinel, log once,
// then answer 500 only when nothing was committed and the client still speaks
// HTTP (not after an Upgrade).
func handlePanic(log *slog.Logger, w http.ResponseWriter, r *http.Request, rvr any) {
	// Let the server handle its own abort sentinel; re-panic it untouched,
	// exactly as net/http does internally. rvr is a recovered value (any), so
	// assert to error before matching.
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
	if rw, ok := w.(*responseWriter); ok && rw.wroteHeader {
		return
	}
	if r.Header.Get("Connection") == "Upgrade" {
		return
	}
	presenter.Error(
		w, r,
		errors.New("recovered from panic"),
		presenter.WithStatus(http.StatusInternalServerError),
	)
}
