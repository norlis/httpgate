package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/norlis/httpgate/logging"
)

var (
	// ErrAlreadyStarted is returned by Start on a running Component.
	ErrAlreadyStarted = errors.New("server: already started")
	// ErrStopped is returned by Start after Stop: an http.Server cannot serve
	// again after Shutdown.
	ErrStopped = errors.New("server: cannot restart after Stop")
)

// Component runs an *http.Server with a Start/Stop lifecycle, for services
// that orchestrate several components (an HTTP server next to consumers, for
// instance) instead of handing control to Run. The bind happens in Start, so
// a busy port fails startup instead of surfacing after the service reported
// itself up.
//
// Stop does not run OnShutdown hooks nor a drain delay: flipping readiness
// and waiting for the load balancer belong to whoever orders the shutdown.
type Component struct {
	srv    *http.Server
	logger *slog.Logger

	mu      sync.Mutex
	addr    net.Addr
	done    chan struct{} // closed when Serve returns
	started bool
	stopped bool
}

// NewComponent wraps s, usually built by New. A nil s or a nil s.Handler
// panics: http.Server would fall back to http.DefaultServeMux and expose
// whatever is registered there, such as net/http/pprof. A nil logger means
// slog.Default(). When s.ErrorLog is unset it is routed through logger, so
// net/http internal errors stay structured.
func NewComponent(s *http.Server, logger *slog.Logger) *Component {
	if s == nil || s.Handler == nil {
		panic("server: NewComponent needs a server with a non-nil Handler")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if s.ErrorLog == nil {
		s.ErrorLog = slog.NewLogLogger(logger.Handler(), slog.LevelError)
	}
	return &Component{srv: s, logger: logger}
}

// Start binds the listen address and serves in the background. A serve
// failure after Start returned goes to onFatal; with a nil onFatal it is
// logged instead, so it is reported exactly once either way. onFatal may call
// Stop synchronously.
func (c *Component) Start(ctx context.Context, onFatal func(error)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.started:
		return ErrAlreadyStarted
	case c.stopped:
		return ErrStopped
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", c.srv.Addr)
	if err != nil {
		return fmt.Errorf("server: listen %s: %w", c.srv.Addr, err)
	}
	c.addr, c.started, c.done = ln.Addr(), true, make(chan struct{})
	c.logger.LogAttrs(ctx, slog.LevelInfo, "server listening", slog.String("server.address", c.addr.String()))
	go c.serve(context.WithoutCancel(ctx), ln, onFatal, c.done)
	return nil
}

// serve runs the server on ln until it is shut down; any other end is a
// failure. ctx outlives Start's cancellation and only carries log values.
// done closes before onFatal runs so an onFatal that calls Stop does not wait
// on itself.
func (c *Component) serve(ctx context.Context, ln net.Listener, onFatal func(error), done chan<- struct{}) {
	err := c.srv.Serve(ln)
	close(done)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return
	}
	err = fmt.Errorf("server: serve: %w", err)
	if onFatal == nil {
		c.logger.LogAttrs(ctx, slog.LevelError, "server failed", logging.Err(err))
		return
	}
	onFatal(err)
}

// Stop shuts the server down, waiting for in-flight requests until ctx
// expires. Stop before Start, or a second Stop, returns nil. A stopped
// Component cannot be started again.
func (c *Component) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started {
		return nil
	}
	c.started, c.stopped = false, true
	err := c.srv.Shutdown(ctx)
	<-c.done // Shutdown closes the listener first, so Serve has returned or is about to
	if err != nil {
		return fmt.Errorf("server: graceful shutdown: %w", err)
	}
	return nil
}

// Addr is the address the server is bound to, with ":0" resolved, or nil
// before Start.
func (c *Component) Addr() net.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.addr
}
