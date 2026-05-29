package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/norlis/httpgate/problem"
)

// InterceptorOptions configures which status codes InterceptStatus rewrites
// and the optional per-code detail messages.
type InterceptorOptions struct {
	codesToIntercept map[int]bool
	customMessages   map[int]string
}

// Option configures an InterceptorOptions.
type Option func(*InterceptorOptions)

// WithIntercept registers the status codes that should be rewritten as RFC
// 7807 problem responses.
func WithIntercept(codes ...int) Option {
	return func(opts *InterceptorOptions) {
		if opts.codesToIntercept == nil {
			opts.codesToIntercept = make(map[int]bool)
		}
		for _, code := range codes {
			opts.codesToIntercept[code] = true
		}
	}
}

// WithMessage attaches a custom detail message to a specific status code.
func WithMessage(code int, message string) Option {
	return func(opts *InterceptorOptions) {
		if opts.customMessages == nil {
			opts.customMessages = make(map[int]string)
		}
		opts.customMessages[code] = message
	}
}

// apiErrorInterceptor is an http.ResponseWriter that intercepts errors per the configuration.
type apiErrorInterceptor struct {
	http.ResponseWriter
	request           *http.Request
	interceptedStatus int
	options           *InterceptorOptions
}

func (w *apiErrorInterceptor) WriteHeader(statusCode int) {
	if w.options.codesToIntercept[statusCode] {
		w.interceptedStatus = statusCode
	} else {
		w.ResponseWriter.WriteHeader(statusCode)
	}
}

func (w *apiErrorInterceptor) Write(p []byte) (int, error) {
	if w.interceptedStatus != 0 {
		return w.writeCustomErrorResponse()
	}
	return w.ResponseWriter.Write(p)
}

func (w *apiErrorInterceptor) writeCustomErrorResponse() (int, error) {
	statusCode := w.interceptedStatus
	w.interceptedStatus = 0

	// Body is RFC 7807 (problem detail). Use the correct media type so
	// callers can content-negotiate against it.
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.ResponseWriter.WriteHeader(statusCode)

	// Detail stays empty unless a custom message was registered for this
	// status; we deliberately avoid problem.FromError here because its
	// fallback would synthesize a detail like "status 500".
	opts := []problem.Option{problem.WithInstance(w.request)}
	if customMsg, ok := w.options.customMessages[statusCode]; ok {
		opts = append(opts, problem.WithDetail(customMsg))
	}

	pb := problem.New(http.StatusText(statusCode), statusCode, opts...)

	jsonData, err := json.Marshal(pb)
	if err != nil {
		http.Error(w.ResponseWriter, "Internal Server Error", http.StatusInternalServerError)
		return 0, err
	}

	return w.ResponseWriter.Write(jsonData)
}

// InterceptStatus builds middleware that rewrites the configured status codes
// into RFC 7807 problem+json responses. Unintercepted codes pass through
// untouched.
func InterceptStatus(opts ...Option) func(http.Handler) http.Handler {
	options := &InterceptorOptions{}
	for _, opt := range opts {
		opt(options)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			interceptor := &apiErrorInterceptor{
				ResponseWriter: w,
				request:        r,
				options:        options,
			}
			next.ServeHTTP(interceptor, r)

			if interceptor.interceptedStatus != 0 {
				_, _ = interceptor.writeCustomErrorResponse()
			}
		})
	}
}
