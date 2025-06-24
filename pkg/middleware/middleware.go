package middleware

import "net/http"

type Middleware func(http.Handler) http.Handler

func Chain(xs ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(xs) - 1; i >= 0; i-- {
			x := xs[i]
			next = x(next)
		}

		return next
	}
}

type WrapResponseWriter interface {
	http.ResponseWriter
	Status() int
}

type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	intercepted bool
}

func NewWrapResponseWriter(w http.ResponseWriter, protoMajor int) WrapResponseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: protoMajor}
}

func (rw *responseWriter) Status() int {
	return rw.statusCode
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
