package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew_appliesSafeDefaults(t *testing.T) {
	t.Parallel()
	s := New(http.NotFoundHandler())
	if s.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", s.Addr)
	}
	if s.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 5s", s.ReadHeaderTimeout)
	}
	if s.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v, want 30s", s.ReadTimeout)
	}
	if s.WriteTimeout != 60*time.Second {
		t.Errorf("WriteTimeout = %v, want 60s", s.WriteTimeout)
	}
	if s.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s (must exceed the load balancer's idle timeout)", s.IdleTimeout)
	}
	if s.MaxHeaderBytes != 64<<10 {
		t.Errorf("MaxHeaderBytes = %d, want %d", s.MaxHeaderBytes, 64<<10)
	}
	if s.MaxHeaderValueCount != 256 {
		t.Errorf("MaxHeaderValueCount = %d, want 256", s.MaxHeaderValueCount)
	}
	if s.HTTP2 == nil {
		t.Fatal("HTTP2 config must be set")
	}
	if s.HTTP2.MaxConcurrentStreams != 250 {
		t.Errorf("HTTP2.MaxConcurrentStreams = %d, want 250", s.HTTP2.MaxConcurrentStreams)
	}
	if s.HTTP2.SendPingTimeout != 30*time.Second || s.HTTP2.PingTimeout != 15*time.Second {
		t.Errorf("HTTP2 ping = %v/%v, want 30s/15s", s.HTTP2.SendPingTimeout, s.HTTP2.PingTimeout)
	}
	if s.HTTP2.WriteByteTimeout != 15*time.Second {
		t.Errorf("HTTP2.WriteByteTimeout = %v, want 15s", s.HTTP2.WriteByteTimeout)
	}
}

func TestNew_optionsOverride(t *testing.T) {
	t.Parallel()
	s := New(
		http.NotFoundHandler(),
		WithAddr(":9999"),
		With(func(s *http.Server) { s.ReadTimeout = time.Second }),
	)
	if s.Addr != ":9999" {
		t.Errorf("Addr = %q, want :9999", s.Addr)
	}
	if s.ReadTimeout != time.Second {
		t.Errorf("ReadTimeout = %v, want 1s", s.ReadTimeout)
	}
}

func TestRun_gracefulShutdownAndOnShutdownHook(t *testing.T) {
	t.Parallel()
	addr := freeAddr(t)
	var hookCalled atomic.Bool
	srv := New(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		WithAddr(addr),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(
			ctx, srv,
			WithLogger(slog.New(slog.DiscardHandler)),
			OnShutdown(func() { hookCalled.Store(true) }),
		)
	}()

	waitUp(t, addr)

	cancel() // simulate SIGTERM via parent context cancellation
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error on clean shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
	if !hookCalled.Load() {
		t.Fatal("OnShutdown hook was not invoked")
	}
}

// freeAddr reserves an OS-assigned port and releases it for Run to bind.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitUp(t *testing.T, addr string) {
	t.Helper()
	for range 50 {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server never came up on %s", addr)
}

func TestRun_wiresErrorLogToStructuredLogger(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	srv := New(http.NotFoundHandler(), WithAddr(freeAddr(t)))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, srv, WithLogger(logger)) }()
	waitUp(t, srv.Addr)
	cancel()
	<-done // Run's return orders its write of srv.ErrorLog before our read

	if srv.ErrorLog == nil {
		t.Fatal("Run must set http.Server.ErrorLog when nil so net/http errors are structured")
	}
	srv.ErrorLog.Print("http: simulated internal error")

	var m map[string]any
	line := strings.TrimSpace(buf.String())
	last := line[strings.LastIndex(line, "\n")+1:]
	if err := json.Unmarshal([]byte(last), &m); err != nil {
		t.Fatalf("ErrorLog output is not JSON: %v\n%s", err, buf.String())
	}
	if m["level"] != "ERROR" || !strings.Contains(m["msg"].(string), "simulated internal error") {
		t.Fatalf("unexpected record: %v", m)
	}
}

func TestRun_doesNotOverrideExplicitErrorLog(t *testing.T) {
	t.Parallel()
	own := slog.NewLogLogger(slog.DiscardHandler, slog.LevelError)
	srv := New(http.NotFoundHandler(), WithAddr(freeAddr(t)), With(func(s *http.Server) { s.ErrorLog = own }))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, srv, WithLogger(slog.New(slog.DiscardHandler))) }()
	waitUp(t, srv.Addr)
	cancel()
	<-done
	if srv.ErrorLog != own {
		t.Fatal("Run replaced an ErrorLog the caller had set explicitly")
	}
}

func TestRun_drainDelayKeepsServingBeforeShutdown(t *testing.T) {
	t.Parallel()
	const delay = 300 * time.Millisecond
	addr := freeAddr(t)
	srv := New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }), WithAddr(addr))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var drainingAt time.Time
	go func() {
		done <- Run(
			ctx, srv,
			WithLogger(slog.New(slog.DiscardHandler)),
			WithDrainDelay(delay),
			OnShutdown(func() { drainingAt = time.Now() }),
		)
	}()
	waitUp(t, addr)
	cancel()

	// While draining, the listener must still accept: this is the window the
	// load balancer needs to stop routing to this instance.
	time.Sleep(delay / 3)
	res, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("request during drain delay failed: %v", err)
	}
	_ = res.Body.Close()

	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := time.Since(drainingAt); got < delay {
		t.Fatalf("Run returned %v after draining, want >= %v", got, delay)
	}
}

func TestRun_bindErrorReturnsAtOnce(t *testing.T) {
	t.Parallel()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := New(http.NotFoundHandler(), WithAddr(ln.Addr().String()))
	if err := Run(t.Context(), srv, WithLogger(slog.New(slog.DiscardHandler))); err == nil {
		t.Fatal("Run() on a busy port error = nil")
	}
}
