package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func noopMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func emptyHandler() http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})
}

// BenchmarkChain_Build measures Chain construction cost.
func BenchmarkChain_Build(b *testing.B) {
	mws := []Middleware{noopMW, noopMW, noopMW, noopMW, noopMW}
	b.ReportAllocs()
	for b.Loop() {
		_ = New(mws...)
	}
}

// BenchmarkChain_Then measures the cost of applying a pre-built chain
// to a handler on each request.
func BenchmarkChain_Then(b *testing.B) {
	chain := New(noopMW, noopMW, noopMW, noopMW, noopMW)
	h := chain.Then(emptyHandler())
	req := httptest.NewRequest("GET", "/", http.NoBody)
	b.ReportAllocs()
	for b.Loop() {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
	}
}
