package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
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
	if s.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %v, want 15s", s.ReadTimeout)
	}
	if s.WriteTimeout != 15*time.Second {
		t.Errorf("WriteTimeout = %v, want 15s", s.WriteTimeout)
	}
	if s.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", s.IdleTimeout)
	}
	if s.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want %d", s.MaxHeaderBytes, 1<<20)
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
