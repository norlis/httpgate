package middleware

import (
	"errors"
	"net/http"
	"runtime/debug"

	"github.com/norlis/httpgate/pkg/presenters"

	"go.uber.org/zap"
)

func Recover(log *zap.Logger, render presenters.Presenters) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					if rvr == http.ErrAbortHandler {
						// we don't recover http.ErrAbortHandler so the response
						// to the client is aborted, this should not be logged
						panic(rvr)
					}
					log.Named("middleware.recover").With().
						Error("Recovered from panic", zap.String("Stacktrace", string(debug.Stack())))

					if r.Header.Get("Connection") != "Upgrade" {
						render.Error(
							w, r,
							errors.New("recovered from panic"),
							presenters.WithStatus(http.StatusInternalServerError),
						)
					}
				}
			}()

			next.ServeHTTP(w, r)
		}

		return http.HandlerFunc(fn)
	}
}
