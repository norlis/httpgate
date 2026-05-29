// Command basic demonstrates wiring httpgate's middleware chain, health
// probes and OPA-backed authorization with graceful shutdown.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/norlis/httpgate/authz"
	"github.com/norlis/httpgate/authz/opa"
	"github.com/norlis/httpgate/health"
	"github.com/norlis/httpgate/middleware"
	"github.com/norlis/httpgate/presenter"
)

const banner = `
▗▄▄▄▖▗▖  ▗▖ ▗▄▖ ▗▖  ▗▖▗▄▄▖ ▗▖   ▗▄▄▄▖
▐▌    ▝▚▞▘ ▐▌ ▐▌▐▛▚▞▜▌▐▌ ▐▌▐▌   ▐▌
▐▛▀▀▘  ▐▌  ▐▛▀▜▌▐▌  ▐▌▐▛▀▘ ▐▌   ▐▛▀▀▘
▐▙▄▄▖▗▞▘▝▚▖▐▌ ▐▌▐▌  ▐▌▐▌   ▐▙▄▄▖▐▙▄▄▖
`

func main() {
	fmt.Print(banner)
	logger := newLogger()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	enforcer, err := opa.New(ctx, opa.Config{
		Query:        "data.authz.allow",
		PoliciesPath: "policies/authz",
	}, opa.WithLogger(logger))
	if err != nil {
		log.Fatalf("opa init: %v", err)
	}

	commons := middleware.New(
		middleware.TraceID(middleware.WithHeaderName("X-Request-ID")),
		middleware.InterceptStatus(
			middleware.WithIntercept(
				http.StatusNotFound,
				http.StatusMethodNotAllowed,
				http.StatusInternalServerError,
			),
			middleware.WithMessage(http.StatusNotFound, "resource not found"),
			middleware.WithMessage(http.StatusMethodNotAllowed, "method not allowed for this resource"),
		),
		middleware.Recover(logger),
		middleware.RequestLogger(logger),
		middleware.AllowAll(),
	)
	public := commons
	protected := commons.Append(
		// CSRFProtect runs BEFORE Authorize: reject cross-origin writes
		// before paying the cost of policy evaluation.
		middleware.CSRFProtect(
			middleware.WithTrustedOrigin("https://app.example.com"),
		),
		middleware.Authorize(enforcer, extractRoles),
	)

	base := http.NewServeMux()
	base.Handle("GET /status", health.NewStatus("dev"))
	base.Handle("GET /live", health.NewProbe(nil))
	base.Handle("GET /ready", health.NewProbe(nil))

	api := http.NewServeMux()
	api.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		presenter.JSON(
			w, r,
			map[string]string{"text": "Hello World"},
			presenter.WithStatusCode(http.StatusAccepted),
			presenter.WithHeader("x-test", "1"),
		)
	})
	api.HandleFunc("GET /test-err", func(w http.ResponseWriter, r *http.Request) {
		presenter.Error(
			w, r, errors.New("error occurred"),
			presenter.WithStatus(http.StatusBadRequest),
		)
	})
	api.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("panic test"))
	})

	root := http.NewServeMux()
	root.Handle("/", public.Then(base))
	root.Handle("/api/", protected.Then(http.StripPrefix("/api", api)))

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)

	srv := &http.Server{
		Addr:              ":8881",
		Handler:           root,
		ReadHeaderTimeout: 30 * time.Second,
		Protocols:         protocols,
	}

	go func() {
		logger.Info("http listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http serve", slog.Any("error", err))
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down", slog.Any("cause", context.Cause(ctx)))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", slog.Any("error", err))
	}
	logger.Info("bye")
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("DEBUG"), "true") {
		level = slog.LevelDebug
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			return a
		},
	})
	return slog.New(h)
}

// extractRoles reads `X-Roles` (CSV) for demo purposes. In production,
// parse a JWT or session cookie here.
func extractRoles(r *http.Request) (map[string]any, error) {
	raw := r.Header.Get("X-Roles")
	roles := []string{}
	if raw != "" {
		for p := range strings.SplitSeq(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				roles = append(roles, p)
			}
		}
	}
	return map[string]any{"roles": roles}, nil
}

// Compile-time check: PayloadExtractor signature.
var _ authz.PayloadExtractor = extractRoles
