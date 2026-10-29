// Package server builds http.Servers with hardened defaults and runs them
// with coordinated, signal-driven graceful shutdown.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// New returns an *http.Server with hardened defaults: anti-Slowloris
// timeouts, bounded headers and per-connection HTTP/2 limits. The global
// Read/WriteTimeout are safety nets; per-route budgets belong to
// middleware.Timeout. IdleTimeout must stay above the load balancer's idle
// timeout (ALB default 60s) or the balancer may reuse a connection the server
// already closed. Override any field via opts.
func New(h http.Handler, opts ...Option) *http.Server {
	s := &http.Server{
		Handler:             h,
		Addr:                ":8080",
		ReadHeaderTimeout:   5 * time.Second,
		ReadTimeout:         30 * time.Second,
		WriteTimeout:        60 * time.Second,
		IdleTimeout:         120 * time.Second,
		MaxHeaderBytes:      64 << 10, // Tomcat/Netty default 8 KiB; 64 KiB leaves room for large JWTs
		MaxHeaderValueCount: 256,
		HTTP2: &http.HTTP2Config{
			MaxConcurrentStreams: 250,              // bounds handler goroutines per connection (rapid-reset)
			SendPingTimeout:      30 * time.Second, // detect dead connections behind NAT/LB
			PingTimeout:          15 * time.Second,
			WriteByteTimeout:     15 * time.Second, // a client that stops reading gets closed, not buffered
		},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Option mutates the server built by New.
type Option func(*http.Server)

// WithAddr sets the listen address (default ":8080").
func WithAddr(addr string) Option { return func(s *http.Server) { s.Addr = addr } }

// With is an escape hatch to set any *http.Server field (Protocols, TLSConfig…).
func With(fn func(*http.Server)) Option { return fn }

// Run binds s, serves it and blocks until ctx is canceled or SIGINT/SIGTERM
// arrives, then drains: it invokes any OnShutdown hooks (e.g. flip a readiness gate to
// 503 so the load balancer stops routing) and waits up to the shutdown timeout
// for in-flight requests. Returns nil on a clean shutdown and an error when
// the bind or the server fails. Services that orchestrate several components
// use Component instead.
func Run(ctx context.Context, s *http.Server, opts ...RunOption) error {
	cfg := runConfig{shutdownTimeout: 25 * time.Second, logger: slog.Default()}
	for _, o := range opts {
		o(&cfg)
	}

	// The bind happens before the signal wait, so a busy port fails here
	// instead of after the service reported itself up.
	c := NewComponent(s, cfg.logger)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	fatal := make(chan error, 1)
	if err := c.Start(ctx, func(err error) { fatal <- err }); err != nil {
		return err
	}

	select {
	case err := <-fatal:
		return err
	case <-ctx.Done():
		stop() // restore default signal handling; a second SIGTERM now kills
		for _, fn := range cfg.onShutdown {
			fn()
		}
		if cfg.drainDelay > 0 {
			cfg.logger.InfoContext(ctx, "server not ready, waiting for load balancer", slog.Duration("drain.delay", cfg.drainDelay))
			time.Sleep(cfg.drainDelay)
		}
		cfg.logger.InfoContext(ctx, "server draining", slog.Duration("shutdown.timeout", cfg.shutdownTimeout))
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.shutdownTimeout)
		defer cancel()
		return c.Stop(sctx)
	}
}

type runConfig struct {
	shutdownTimeout time.Duration
	drainDelay      time.Duration
	logger          *slog.Logger
	onShutdown      []func()
}

// RunOption configures Run.
type RunOption func(*runConfig)

// WithShutdownTimeout caps how long Run waits for in-flight requests to finish.
// Keep it below the platform's stop timeout (ECS/Fargate default 30s).
func WithShutdownTimeout(d time.Duration) RunOption {
	return func(c *runConfig) { c.shutdownTimeout = d }
}

// WithDrainDelay makes Run keep serving for d after the OnShutdown hooks run
// (readiness already reports 503) and before Shutdown closes the listener.
// Kubernetes needs a few seconds to remove the pod from Endpoints; without
// this pause new connections arriving in that window are refused. Keep
// drainDelay + shutdownTimeout below the platform's termination grace period.
// Default 0 (no pause); 3–5s is typical on Kubernetes, 0 on ECS behind an ALB.
func WithDrainDelay(d time.Duration) RunOption {
	return func(c *runConfig) { c.drainDelay = d }
}

// WithLogger sets the logger used for lifecycle events. A nil logger is ignored.
func WithLogger(l *slog.Logger) RunOption {
	return func(c *runConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// OnShutdown registers a hook run at the start of draining, before Shutdown.
// Use it to flip a readiness gate so the load balancer drains first.
func OnShutdown(fn func()) RunOption {
	return func(c *runConfig) { c.onShutdown = append(c.onShutdown, fn) }
}
