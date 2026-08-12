// Command basic demonstrates wiring httpgate's middleware chain, health
// probes and OPA-backed authorization with graceful shutdown.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/norlis/httpgate/authz/opa"
	"github.com/norlis/httpgate/health"
	"github.com/norlis/httpgate/logging"
	"github.com/norlis/httpgate/middleware"
	"github.com/norlis/httpgate/server"
	"github.com/norlis/httpgate/trace"
)

const banner = `
▗▄▄▄▖▗▖  ▗▖ ▗▄▖ ▗▖  ▗▖▗▄▄▖ ▗▖   ▗▄▄▄▖
▐▌    ▝▚▞▘ ▐▌ ▐▌▐▛▚▞▜▌▐▌ ▐▌▐▌   ▐▌
▐▛▀▀▘  ▐▌  ▐▛▀▜▌▐▌  ▐▌▐▛▀▘ ▐▌   ▐▛▀▀▘
▐▙▄▄▖▗▞▘▝▚▖▐▌ ▐▌▐▌  ▐▌▐▌   ▐▙▄▄▖▐▙▄▄▖
`

func main() {
	fmt.Print(banner)

	// 12-factor: configuration comes from the environment.
	logger := logging.New(
		os.Stdout,
		logging.WithService(envOr("SERVICE_NAME", "basic"), envOr("SERVICE_VERSION", "dev")),
		logging.WithEnvironment(envOr("ENVIRONMENT", "development")),
		logging.WithLevel(levelFromEnv()),
	)

	// server.Run installs its own SIGINT/SIGTERM handling; a plain context
	// is enough for initialization here.
	ctx := context.Background()

	enforcer, err := opa.New(ctx, opa.Config{
		Query:        "data.authz.allow",
		PoliciesPath: "policies/authz",
	}, opa.WithLogger(logger))
	if err != nil {
		// FATAL semantics: the process cannot start without its policy engine.
		logger.Error("opa initialization failed", logging.Err(err))
		os.Exit(1)
	}

	commons := middleware.New(
		middleware.TraceContext(middleware.WithResponseHeader("X-Request-ID")),
		middleware.InterceptStatus(
			middleware.WithIntercept(
				http.StatusNotFound,
				http.StatusMethodNotAllowed,
				http.StatusInternalServerError,
			),
			middleware.WithMessage(http.StatusNotFound, "resource not found"),
			middleware.WithMessage(http.StatusMethodNotAllowed, "method not allowed for this resource"),
		),
		middleware.RequestLogger(logger, middleware.WithSkipPaths("/status", "/live", "/ready")),
		middleware.Recover(logger),
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

	// Outbound calls made with this client propagate traceparent automatically.
	outboundClient := &http.Client{Transport: &trace.Transport{}, Timeout: 5 * time.Second}

	base := http.NewServeMux()
	base.Handle("GET /status", health.NewStatus("dev"))
	base.Handle("GET /live", health.NewProbe(nil))
	base.Handle("GET /ready", ready)
	base.HandleFunc("GET /me/permissions", permissionsHandler(enforcer, logger))

	api := http.NewServeMux()
	api.HandleFunc("GET /test", helloHandler())
	api.HandleFunc("GET /test-err", errorHandler())
	api.HandleFunc("GET /panic", panicHandler())
	api.HandleFunc("GET /outbound", outboundHandler(outboundClient))

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
		logger.Error("server run failed", logging.Err(err))
		os.Exit(1)
	}
	logger.Info("bye")
}

// envOr returns the env var value or a fallback for local runs.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// levelFromEnv maps LOG_LEVEL (debug|info|warn|error) to a slog.Level,
// defaulting to info on absence or garbage.
func levelFromEnv() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(envOr("LOG_LEVEL", "info"))); err != nil {
		return slog.LevelInfo
	}
	return l
}
