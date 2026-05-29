package middleware

import (
	"net/http"
	"slices"
)

// Middleware is the constructor signature shared by every middleware in
// this package. It takes the next http.Handler and returns a handler
// that wraps it.
type Middleware func(http.Handler) http.Handler

// Chain is an immutable list of middlewares plus the methods to compose
// them. A zero-value Chain is valid: it represents the empty chain.
//
// All "mutating" methods return a new Chain, leaving the receiver
// untouched, so a Chain value is safe to share, reuse, and pass by value.
type Chain struct {
	mws []Middleware
}

// New builds a Chain from the given middlewares. The middlewares execute
// in the order given:
//
//	New(a, b, c).Then(h)  ==  a(b(c(h)))
//
// New defensively copies the input slice so subsequent mutation of the
// caller's slice does not leak into the Chain.
func New(mws ...Middleware) Chain {
	return Chain{mws: append([]Middleware(nil), mws...)}
}

// Then applies the chain to h and returns the resulting handler.
// Then(nil) defaults to http.DefaultServeMux, matching stdlib conventions.
//
// Then may be called many times on the same Chain. Each call re-invokes
// every middleware constructor, which is the desired behavior when
// middlewares capture per-pipeline state.
func (c Chain) Then(h http.Handler) http.Handler {
	if h == nil {
		h = http.DefaultServeMux
	}
	for _, mw := range slices.Backward(c.mws) {
		h = mw(h)
	}
	return h
}

// ThenFunc is the http.HandlerFunc shortcut for Then.
//
//	c.ThenFunc(fn)  ==  c.Then(http.HandlerFunc(fn))
//
// A nil fn is treated as a nil handler (defaults to http.DefaultServeMux).
func (c Chain) ThenFunc(fn http.HandlerFunc) http.Handler {
	if fn == nil {
		return c.Then(nil)
	}
	return c.Then(fn)
}

// Append returns a new Chain with the given middlewares appended after
// the existing ones. The receiver is not modified.
func (c Chain) Append(mws ...Middleware) Chain {
	out := make([]Middleware, 0, len(c.mws)+len(mws))
	out = append(out, c.mws...)
	out = append(out, mws...)
	return Chain{mws: out}
}

// Extend returns a new Chain that runs c's middlewares followed by
// other's. Neither receiver is modified.
func (c Chain) Extend(other Chain) Chain {
	return c.Append(other.mws...)
}

// WrapResponseWriter wraps an http.ResponseWriter to expose the status
// code and the number of bytes written.
type WrapResponseWriter interface {
	http.ResponseWriter
	Status() int
	BytesWritten() int
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

// WrapWriter wraps w. The returned writer reports status code (default
// 200) and bytes written.
func WrapWriter(w http.ResponseWriter) WrapResponseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) Status() int       { return rw.statusCode }
func (rw *responseWriter) BytesWritten() int { return rw.bytes }

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(p []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(p)
	rw.bytes += n
	return n, err
}
