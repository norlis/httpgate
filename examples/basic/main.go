// Command basic demonstrates wiring httpgate's middleware chain, health
// probes and OPA-backed authorization with graceful shutdown.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/norlis/httpgate/authz/opa"
	"github.com/norlis/httpgate/health"
	"github.com/norlis/httpgate/middleware"
	"github.com/norlis/httpgate/server"
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

	// server.Run installs its own SIGINT/SIGTERM handling; a plain context
	// is enough for initialization here.
	ctx := context.Background()

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

	// readiness gate: server.Run flips it to 503 on SIGTERM so the LB drains
	// before connections are closed.
	ready := health.NewReadiness(health.NewProbe(nil))

	base := http.NewServeMux()
	base.Handle("GET /status", health.NewStatus("dev"))
	base.Handle("GET /live", health.NewProbe(nil))
	base.Handle("GET /ready", ready)
	base.HandleFunc("GET /me/permissions", permissionsHandler(enforcer, logger))

	api := http.NewServeMux()
	api.HandleFunc("GET /test", helloHandler())
	api.HandleFunc("GET /test-err", errorHandler())
	api.HandleFunc("GET /panic", panicHandler())

	root := http.NewServeMux()
	root.Handle("/", public.Then(base))
	root.Handle("/api/", protected.Then(http.StripPrefix("/api", api)))

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)

	srv := server.New(
		root,
		server.WithAddr(":8881"),
		server.With(func(s *http.Server) { s.Protocols = protocols }),
	)
	if err := server.Run(
		ctx, srv,
		server.WithLogger(logger),
		server.OnShutdown(ready.MarkDraining),
	); err != nil {
		logger.Error("server", slog.Any("error", err))
		os.Exit(1)
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
