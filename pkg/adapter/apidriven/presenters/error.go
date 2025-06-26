package presenters

import (
	"net/http"

	"github.com/norlis/httpgate/pkg/kit/problem"

	"go.uber.org/zap"
)

type errorConfig struct {
	status int
	title  string
	detail string
}

// ErrorOption es una función que modifica la configuración del error.
type ErrorOption func(*errorConfig)

// WithStatus es una opción para establecer el código de estado HTTP del error.
func WithStatus(status int) ErrorOption {
	return func(c *errorConfig) {
		c.status = status
	}
}

// WithTitle es una opción para establecer el título del problema.
func WithTitle(title string) ErrorOption {
	return func(c *errorConfig) {
		c.title = title
	}
}

// WithDetail es una opción para establecer el detalle del problema.
func WithDetail(detail string) ErrorOption {
	return func(c *errorConfig) {
		c.detail = detail
	}
}

// Error genera una respuesta de error estandarizada usando el patrón de opciones.
func (p *presenters) Error(w http.ResponseWriter, r *http.Request, err error, opts ...ErrorOption) {
	config := &errorConfig{
		status: http.StatusInternalServerError,
	}

	for _, opt := range opts {
		opt(config)
	}

	if config.title == "" {
		config.title = http.StatusText(config.status)
	}

	if config.detail == "" && err != nil {
		config.detail = err.Error()
	}

	if config.detail == "" {
		config.detail = "unknown error"
	}

	if config.status >= 500 {
		p.log.Error("server error occurred",
			zap.Error(err), // El error original
			zap.Int("status", config.status),
			zap.String("final_detail", config.detail), // El detalle que se envía al cliente
		)
	}

	pd := problem.New(
		config.title,
		config.status,
		problem.WithDetail(config.detail),
		problem.WithInstance(r),
	)

	// TODO agregar el requestId del contexto
	//if requestID, ok := r.Context().Value(middleware.RequestIDKey).(string); ok {
	//	pd.RequestId = requestID
	//}

	problem.RespondError(w, pd)
}
