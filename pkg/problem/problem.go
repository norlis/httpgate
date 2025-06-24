// Package problem - RFC 7807 (Problem Details for HTTP APIs).
// https://tools.ietf.org/html/rfc7807
package problem

import (
	"encoding/json"
	"net/http"
	"time"
)

// ProblemDetail define la estructura estándar para un error HTTP, según el RFC 7807.
// El struct satisface la interfaz `error` de Go, por lo que puede ser usado
// en flujos de manejo de errores estándar.
type ProblemDetail struct {
	// Type es un URI que identifica el tipo de problema.
	// Se recomienda que provea documentación legible para los desarrolladores.
	Type string `json:"type,omitempty"`

	// Title es un resumen corto y legible del tipo de problema.
	// No debería cambiar entre distintas ocurrencias del mismo problema.
	Title string `json:"title"`

	// Status es el código de estado HTTP reflejado para esta ocurrencia del problema.
	Status int `json:"status"`

	// Detail es una explicación legible y específica de esta ocurrencia del problema.
	Detail string `json:"detail,omitempty"`

	// Instance es un URI que identifica la ocurrencia específica del problema.
	Instance string `json:"instance,omitempty"`

	Extension
}

type Extension struct {
	RequestId  string    `json:"requestId,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	StackTrace string    `json:"stackTrace,omitempty"`
}

func (p *ProblemDetail) Error() string {
	return p.Title
}

type Option func(*ProblemDetail)

func New(title string, status int, opts ...Option) *ProblemDetail {
	p := &ProblemDetail{
		Title:  title,
		Status: status,
		Extension: Extension{
			Timestamp: time.Now().UTC(),
		},
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

func FromError(err error, status int, opts ...Option) *ProblemDetail {
	p := New(http.StatusText(status), status, opts...)
	if p.Detail == "" {
		p.Detail = err.Error()
	}
	return p
}

func WithType(uri string) Option {
	return func(p *ProblemDetail) {
		p.Type = uri
	}
}

// WithDetail asigna una explicación específica de la ocurrencia del problema.
func WithDetail(detail string) Option {
	return func(p *ProblemDetail) {
		p.Detail = detail
	}
}

// WithInstance asigna el URI de la petición actual como la instancia del problema.
func WithInstance(r *http.Request) Option {
	return func(p *ProblemDetail) {
		if r != nil {
			p.RequestId = r.Header.Get("X-Request-Id")
			p.Instance = r.URL.Path
		}
	}
}

// RespondError serializa un ProblemDetail a JSON y lo escribe en el http.ResponseWriter.
// Centraliza la lógica de respuesta de errores, asegurando consistencia.
func RespondError(w http.ResponseWriter, p *ProblemDetail) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
