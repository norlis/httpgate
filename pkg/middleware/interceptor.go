package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/norlis/httpgate/pkg/problem"
)

type ErrorMessage struct {
	Message string
}

// InterceptorOptions contiene la configuración para nuestro middleware de errores.
type InterceptorOptions struct {
	codesToIntercept map[int]bool
	customMessages   map[int]ErrorMessage
}

// Option es el tipo de función para el patrón de opciones funcionales.
type Option func(*InterceptorOptions)

// WithIntercept es una opción para especificar qué códigos de estado HTTP interceptar.
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

// WithCustomMessage es una opción para proveer un título y detalle personalizados para un código.
func WithCustomMessage(code int, message string) Option {
	return func(opts *InterceptorOptions) {
		if opts.customMessages == nil {
			opts.customMessages = make(map[int]ErrorMessage)
		}
		opts.customMessages[code] = ErrorMessage{Message: message}
	}
}

// apiErrorInterceptor es un http.ResponseWriter que intercepta errores según la configuración.
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

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.ResponseWriter.WriteHeader(statusCode)

	var detail string
	if customMsg, ok := w.options.customMessages[statusCode]; ok {
		detail = customMsg.Message
	}

	pb := problem.FromError(
		fmt.Errorf("status %d", statusCode),
		statusCode,
		problem.WithDetail(detail),
		problem.WithInstance(w.request),
	)

	jsonData, err := json.Marshal(pb)
	if err != nil {
		http.Error(w.ResponseWriter, "Internal Server Error", http.StatusInternalServerError)
		return 0, err
	}

	return w.ResponseWriter.Write(jsonData)
}

// APIErrorMiddleware es un constructor que crea el middleware a partir de opciones.
func APIErrorMiddleware(opts ...Option) func(http.Handler) http.Handler {
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
