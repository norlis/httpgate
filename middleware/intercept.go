package middleware

import (
	"encoding/json/v2"
	"net/http"

	"github.com/norlis/httpgate/problem"
)

// interceptConfig holds which status codes InterceptStatus rewrites and the
// optional per-code detail messages.
type interceptConfig struct {
	codesToIntercept map[int]bool
	customMessages   map[int]string
}

// InterceptOption configures InterceptStatus.
type InterceptOption func(*interceptConfig)

// WithIntercept registers the status codes that should be rewritten as RFC
// 9457 problem responses.
func WithIntercept(codes ...int) InterceptOption {
	return func(opts *interceptConfig) {
		if opts.codesToIntercept == nil {
			opts.codesToIntercept = make(map[int]bool)
		}
		for _, code := range codes {
			opts.codesToIntercept[code] = true
		}
	}
}

// WithMessage attaches a custom detail message to a specific status code.
func WithMessage(code int, message string) InterceptOption {
	return func(opts *interceptConfig) {
		if opts.customMessages == nil {
			opts.customMessages = make(map[int]string)
		}
		opts.customMessages[code] = message
	}
}

// interceptState is the per-request state InterceptStatus hangs on the shared
// responseWriter. Three states: pass-through (status == 0 && !written),
// pending (status != 0) and written (the problem document is on the wire).
type interceptState struct {
	request *http.Request
	options *interceptConfig
	status  int  // intercepted status held back from the real writer; 0 = none
	written bool // problem document written: drop any further handler output
}

// interceptWriter co-allocates the wrapper and its intercept state for the
// common case where InterceptStatus is the first middleware to wrap w.
type interceptWriter struct {
	responseWriter
	state interceptState
}

// active reports whether handler output must be replaced or dropped.
func (s *interceptState) active() bool { return s.status != 0 || s.written }

// writeIntercepted replaces the handler's response with the RFC 9457 document
// for the pending status. Called at most once per request.
func (rw *responseWriter) writeIntercepted() error {
	s := rw.intercept
	statusCode := s.status
	s.status = 0
	s.written = true

	// Detail stays empty unless a custom message was registered for this
	// status; we deliberately avoid problem.FromError here because its
	// fallback would synthesize a detail like "status 500".
	opts := []problem.Option{problem.WithInstance(s.request)}
	if customMsg, ok := s.options.customMessages[statusCode]; ok {
		opts = append(opts, problem.WithDetail(customMsg))
	}

	// Marshal before committing the header so a marshal failure does not
	// leave a half-written response.
	jsonData, err := json.Marshal(problem.New(http.StatusText(statusCode), statusCode, opts...))
	if err != nil {
		rw.ResponseWriter.WriteHeader(http.StatusInternalServerError)
		return err
	}

	dropEntityHeaders(rw.Header())
	rw.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	rw.ResponseWriter.WriteHeader(statusCode)
	n, err := rw.ResponseWriter.Write(jsonData)
	rw.bytes += n // the access line reports what went on the wire
	return err
}

// InterceptStatus builds middleware that rewrites the configured status codes
// into RFC 9457 problem+json responses. Unintercepted codes pass through
// untouched.
func InterceptStatus(opts ...InterceptOption) Middleware {
	options := &interceptConfig{}
	for _, opt := range opts {
		opt(options)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var ww *responseWriter
			if rw, ok := w.(*responseWriter); ok && rw.intercept == nil {
				// An outer middleware already wrapped w: share its wrapper.
				ww = rw
				ww.intercept = &interceptState{request: r, options: options}
			} else {
				// First wrapper for this request (or a nested InterceptStatus,
				// kept independent): wrapper and state in one allocation.
				iw := &interceptWriter{
					ResponseWriter: w, statusCode: http.StatusOK,
					state: interceptState{request: r, options: options},
				}
				iw.intercept = &iw.state
				ww = &iw.responseWriter
			}
			next.ServeHTTP(ww, r)

			if ww.intercept.status != 0 {
				_ = ww.writeIntercepted()
			}
		})
	}
}

// dropEntityHeaders removes headers that describe the body the handler meant
// to send, not the problem document we are about to write: a stale
// Content-Length truncates or corrupts the replacement response.
func dropEntityHeaders(h http.Header) {
	h.Del("Content-Length")
	h.Del("Content-Encoding")
	h.Del("ETag")
}
