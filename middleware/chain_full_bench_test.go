package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/norlis/httpgate/presenter"
	"github.com/norlis/httpgate/trace"
)

// nullWriter is an http.ResponseWriter that discards everything so the
// benchmarks measure the middlewares, not httptest.ResponseRecorder. The header
// map is reused across iterations and cleared by the loop.
type nullWriter struct{ h http.Header }

func (n *nullWriter) Header() http.Header         { return n.h }
func (n *nullWriter) Write(p []byte) (int, error) { return len(p), nil }
func (n *nullWriter) WriteHeader(int)             {}

func jsonHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presenter.JSON(w, r, map[string]string{"text": "Hello World"})
	})
}

// recommendedChain is the order documented in README/spec: TraceContext →
// InterceptStatus → RequestLogger → Recover → Shed → Timeout → CORS → CSRF → Authorize.
func recommendedChain(withAuthz bool) Chain {
	log := slog.New(slog.DiscardHandler)
	c := New(
		TraceContext(WithResponseHeader("X-Request-ID")),
		InterceptStatus(WithIntercept(http.StatusNotFound, http.StatusInternalServerError)),
		RequestLogger(log),
		Recover(log),
		Shed(256),
		Timeout(10*time.Second),
		AllowAll(),
		CSRFProtect(WithTrustedOrigin("https://app.example.com")),
	)
	if withAuthz {
		c = c.Append(Authorize(&fakeEnforcer{allow: true}, rolesExtractor, WithAuthorizeLogger(log)))
	}
	return c
}

func benchRequest(withTraceparent bool) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/items?page=2", http.NoBody)
	req.Header.Set("X-Roles", "admin")
	req.Header.Set("Origin", "https://app.example.com")
	if withTraceparent {
		req.Header.Set(trace.Header, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	}
	return req
}

func runChain(b *testing.B, h http.Handler, req *http.Request) {
	b.Helper()
	w := &nullWriter{h: make(http.Header, 8)}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.h)
		h.ServeHTTP(w, req)
	}
}

// BenchmarkRecommendedChain measures one full request through the documented
// chain over a presenter.JSON handler. This is the number to compare before and
// after any change on the request hot path.
func BenchmarkRecommendedChain(b *testing.B) {
	b.Run("entrypoint", func(b *testing.B) { runChain(b, recommendedChain(true).Then(jsonHandler()), benchRequest(false)) })
	b.Run("inherited_trace", func(b *testing.B) { runChain(b, recommendedChain(true).Then(jsonHandler()), benchRequest(true)) })
	b.Run("without_authorize", func(b *testing.B) { runChain(b, recommendedChain(false).Then(jsonHandler()), benchRequest(false)) })
	b.Run("handler_only", func(b *testing.B) { runChain(b, jsonHandler(), benchRequest(false)) })
}

// BenchmarkMiddlewareEach isolates the per-request cost of every middleware
// over an empty handler, so a regression can be attributed to one of them.
func BenchmarkMiddlewareEach(b *testing.B) {
	log := slog.New(slog.DiscardHandler)
	each := []struct {
		name string
		mw   Middleware
	}{
		{"TraceContext", TraceContext(WithResponseHeader("X-Request-ID"))},
		{"InterceptStatus", InterceptStatus(WithIntercept(http.StatusNotFound))},
		{"RequestLogger", RequestLogger(log)},
		{"Recover", Recover(log)},
		{"Shed", Shed(256)},
		{"Timeout", Timeout(10 * time.Second)},
		{"CORS", AllowAll()},
		{"CSRFProtect", CSRFProtect(WithTrustedOrigin("https://app.example.com"))},
		{"Authorize", Authorize(&fakeEnforcer{allow: true}, rolesExtractor)},
	}
	req := benchRequest(true)
	for _, e := range each {
		b.Run(e.name, func(b *testing.B) { runChain(b, e.mw(emptyHandler()), req) })
	}
}
