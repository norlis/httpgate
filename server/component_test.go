package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func localComponent(h http.Handler) *Component {
	return NewComponent(New(h, WithAddr("127.0.0.1:0")), discard())
}

func TestComponent_startServesOnResolvedAddr(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	c := NewComponent(
		New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), WithAddr("127.0.0.1:0")),
		slog.New(slog.NewJSONHandler(&buf, nil)),
	)
	if c.Addr() != nil {
		t.Errorf("Addr() before Start = %v, want nil", c.Addr())
	}
	mustStart(t, c)

	if status := get(t, c.Addr().String()); status != http.StatusNoContent {
		t.Errorf("status = %d, want 204", status)
	}
	if want := `"server.address":"` + c.Addr().String() + `"`; !strings.Contains(buf.String(), want) {
		t.Errorf("listening record lacks the resolved address %s: %s", want, buf.String())
	}
}

func TestComponent_bindErrorFailsStart(t *testing.T) {
	t.Parallel()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var fatal atomic.Bool
	c := NewComponent(New(http.NotFoundHandler(), WithAddr(ln.Addr().String())), discard())
	if err := c.Start(t.Context(), func(error) { fatal.Store(true) }); err == nil {
		t.Fatal("Start() on a busy port error = nil")
	}
	if fatal.Load() {
		t.Error("onFatal called for a bind error")
	}
	// A failed Start leaves the component unstarted: Stop is a no-op.
	if err := c.Stop(t.Context()); err != nil {
		t.Errorf("Stop() after a failed Start error = %v", err)
	}
}

func TestComponent_stopDrainsInFlight(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	c := localComponent(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	if err := c.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var status int
	wg.Go(func() { status = get(t, c.Addr().String()) })
	<-entered

	stopErr := make(chan error, 1)
	wg.Go(func() {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		stopErr <- c.Stop(ctx)
	})
	close(release)
	wg.Wait()
	if status != http.StatusOK {
		t.Errorf("in-flight status = %d, want 200", status)
	}
	if err := <-stopErr; err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestComponent_stopExpiredContextReportsError(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	c := localComponent(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
	}))
	if err := c.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	// The client outlives the test body, so it must not report through t.
	go func() {
		if resp, err := http.Get("http://" + c.Addr().String()); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := c.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want DeadlineExceeded", err)
	}
}

// failingListener accepts nothing: Accept fails at once, as a dead socket does.
type failingListener struct{}

func (failingListener) Accept() (net.Conn, error) { return nil, errBoom }
func (failingListener) Close() error              { return nil }
func (failingListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestComponent_serveFailureGoesToOnFatal(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	c := NewComponent(New(http.NotFoundHandler()), slog.New(slog.NewJSONHandler(&buf, nil)))
	var got []error
	c.serve(t.Context(), failingListener{}, func(err error) { got = append(got, err) }, make(chan struct{}))

	if len(got) != 1 || !errors.Is(got[0], errBoom) {
		t.Fatalf("onFatal received %v, want one boom", got)
	}
	if strings.Contains(buf.String(), `"msg":"server failed"`) {
		t.Errorf("a failure reported to onFatal was also logged: %s", buf.String())
	}
}

func TestComponent_serveFailureLoggedWithoutOnFatal(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	c := NewComponent(New(http.NotFoundHandler()), slog.New(slog.NewJSONHandler(&buf, nil)))
	c.serve(t.Context(), failingListener{}, nil, make(chan struct{}))

	if n := strings.Count(buf.String(), `"msg":"server failed"`); n != 1 {
		t.Errorf("logged %d failure records, want 1: %s", n, buf.String())
	}
}

// An onFatal that stops the component synchronously, as a lifecycle group
// does, must not deadlock.
func TestComponent_stopFromOnFatalDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	c := localComponent(http.NotFoundHandler())
	c.started = true
	done := make(chan struct{})
	c.done = done
	returned := make(chan struct{})
	go func() {
		c.serve(t.Context(), failingListener{}, func(error) { _ = c.Stop(context.Background()) }, done)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop from onFatal deadlocked")
	}
}

// mustStart starts c and stops it at cleanup; a Stop already done is a no-op.
func mustStart(t *testing.T, c *Component) {
	t.Helper()
	if err := c.Start(t.Context(), nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
}

func TestComponent_lifecycleMisuse(t *testing.T) {
	t.Parallel()
	t.Run("stop without start", func(t *testing.T) {
		t.Parallel()
		if err := localComponent(http.NotFoundHandler()).Stop(t.Context()); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	})
	t.Run("stop twice", func(t *testing.T) {
		t.Parallel()
		c := localComponent(http.NotFoundHandler())
		mustStart(t, c)
		if err := errors.Join(c.Stop(t.Context()), c.Stop(t.Context())); err != nil {
			t.Fatalf("Stop() twice error = %v", err)
		}
	})
	t.Run("start twice", func(t *testing.T) {
		t.Parallel()
		c := localComponent(http.NotFoundHandler())
		mustStart(t, c)
		if err := c.Start(t.Context(), nil); !errors.Is(err, ErrAlreadyStarted) {
			t.Fatalf("second Start() error = %v, want ErrAlreadyStarted", err)
		}
	})
	t.Run("start after stop", func(t *testing.T) {
		t.Parallel()
		c := localComponent(http.NotFoundHandler())
		mustStart(t, c)
		_ = c.Stop(t.Context())
		if err := c.Start(t.Context(), nil); !errors.Is(err, ErrStopped) {
			t.Fatalf("Start() after Stop error = %v, want ErrStopped", err)
		}
	})
}

func TestNewComponent_nilHandlerPanics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		srv  *http.Server
	}{
		{name: "nil server", srv: nil},
		{name: "nil handler", srv: New(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r, ok := recover().(string); !ok || !strings.HasPrefix(r, "server:") {
					t.Errorf("recover() = %v, want a server panic", r)
				}
			}()
			NewComponent(tt.srv, nil)
		})
	}
}

func TestNewComponent_errorLog(t *testing.T) {
	t.Parallel()
	t.Run("wired to the structured logger when unset", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		srv := New(http.NotFoundHandler())
		NewComponent(srv, slog.New(slog.NewJSONHandler(&buf, nil)))
		if srv.ErrorLog == nil {
			t.Fatal("ErrorLog = nil, want the structured logger")
		}
		srv.ErrorLog.Print("http: simulated internal error")
		if !strings.Contains(buf.String(), `"level":"ERROR"`) || !strings.Contains(buf.String(), "simulated internal error") {
			t.Fatalf("unexpected record: %s", buf.String())
		}
	})
	t.Run("explicit ErrorLog kept", func(t *testing.T) {
		t.Parallel()
		own := slog.NewLogLogger(slog.DiscardHandler, slog.LevelError)
		srv := New(http.NotFoundHandler(), With(func(s *http.Server) { s.ErrorLog = own }))
		NewComponent(srv, discard())
		if srv.ErrorLog != own {
			t.Fatal("NewComponent replaced an explicit ErrorLog")
		}
	})
}

// get returns the status of GET http://addr, or 0 after reporting the error.
func get(t *testing.T, addr string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr, http.NoBody)
	if err != nil {
		t.Error(err)
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("request error = %v", err)
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}
