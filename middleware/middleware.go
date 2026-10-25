// Package middleware provides the composable net/http middlewares of httpgate
// (trace context, access logging, panic recovery, load shedding, timeouts,
// CORS, CSRF, authorization, status interception) and the Chain to assemble
// them.
package middleware

import (
	"io"
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

// responseWriter is the single ResponseWriter wrapper of this package. Every
// middleware that needs response state (RequestLogger, Timeout,
// InterceptStatus) shares one instance per request through WrapWriter, so
// "has the header been committed?" is answered in exactly one place.
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	bytes       int
	wroteHeader bool            // header committed (or intercepted); read by Timeout
	intercept   *interceptState // nil unless InterceptStatus owns this wrapper
}

// WrapWriter wraps w. The returned writer reports status code (default
// 200) and bytes written. It is idempotent: wrapping a writer that this
// package already wrapped returns the existing wrapper, so stacked
// middlewares observe the same state instead of each keeping their own.
func WrapWriter(w http.ResponseWriter) WrapResponseWriter {
	if rw, ok := w.(*responseWriter); ok {
		return rw
	}
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) Status() int       { return rw.statusCode }
func (rw *responseWriter) BytesWritten() int { return rw.bytes }

// Unwrap exposes the underlying writer so http.NewResponseController can
// discover Flush, Hijack, deadlines, etc. through the wrapper (Go 1.20+).
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

func (rw *responseWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 {
		rw.ResponseWriter.WriteHeader(code) // 1xx (e.g. 103 Early Hints) is informational, not the final status
		return
	}
	if rw.wroteHeader {
		if rw.intercept == nil {
			rw.ResponseWriter.WriteHeader(code) // let net/http log the superfluous call
		}
		// With an intercept pending or written, forwarding would commit a
		// different status ahead of the problem document: first status wins.
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
	if rw.intercept != nil && rw.intercept.options.codesToIntercept[code] {
		rw.intercept.status = code // held back; materialized on Write/Flush/handler return
		return
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(p []byte) (int, error) {
	if s := rw.intercept; s != nil && s.active() {
		// The problem document replaces the handler's body: materialize it on
		// the first write and swallow everything else, reporting a full write
		// so the handler never sees a short write.
		if s.status != 0 {
			if err := rw.writeIntercepted(); err != nil {
				return 0, err
			}
		}
		return len(p), nil
	}
	rw.wroteHeader = true
	n, err := rw.ResponseWriter.Write(p)
	rw.bytes += n
	return n, err
}

// Flush is implemented directly (not only via Unwrap) because streaming code
// in the wild still type-asserts w.(http.Flusher). It forwards through
// http.ResponseController so writers that expose flushing only via Unwrap
// (the Go 1.20+ idiom) are still reached; the header counts as committed only
// when the flush actually happened. A pending intercepted status is written
// first: otherwise the flush would commit an implicit 200.
func (rw *responseWriter) Flush() {
	if rw.intercept != nil && rw.intercept.status != 0 {
		_ = rw.writeIntercepted()
	}
	if err := http.NewResponseController(rw.ResponseWriter).Flush(); err == nil {
		rw.wroteHeader = true
	}
}

// ReadFrom forwards to the underlying writer so net/http can keep using
// sendfile for io.Copy(w, file); the fallback avoids recursing into ReadFrom.
func (rw *responseWriter) ReadFrom(src io.Reader) (int64, error) {
	if s := rw.intercept; s != nil && s.active() {
		if s.status != 0 {
			if err := rw.writeIntercepted(); err != nil {
				return 0, err
			}
		}
		return io.Copy(io.Discard, src) //nolint:wrapcheck // same contract as Write: the handler sees a full copy and the raw io error
	}
	rw.wroteHeader = true
	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(src)
		rw.bytes += int(n)
		return n, err //nolint:wrapcheck // pass-through of the wrapped ResponseWriter's error; wrapping would break the io.ReaderFrom contract
	}
	n, err := io.Copy(struct{ io.Writer }{rw.ResponseWriter}, src)
	rw.bytes += int(n)
	return n, err //nolint:wrapcheck // same pass-through as above
}
