package presenter

import (
	"encoding/json/v2"
	"errors"
	"net/http"
)

// Binder is implemented by request DTOs that decode themselves from r.
type Binder interface {
	Bind(r *http.Request) error
}

// DefaultMaxBodyBytes caps the request body Bind reads: 1 MiB. Spring Boot
// uses 2 MB for forms; JSON APIs rarely need more and the cap is per request.
const DefaultMaxBodyBytes int64 = 1 << 20

type bindConfig struct {
	maxBytes int64
}

// BindOption configures Bind.
type BindOption func(*bindConfig)

// WithMaxBodyBytes overrides DefaultMaxBodyBytes for this call.
func WithMaxBodyBytes(n int64) BindOption {
	return func(c *bindConfig) { c.maxBytes = n }
}

// BindError is returned by Bind. Status is the HTTP status the failure maps
// to: 413 when the body exceeded the limit, 400 for any decode or Bind error.
type BindError struct {
	Status int
	Err    error
}

func (e *BindError) Error() string { return "presenter: bind: " + e.Err.Error() }
func (e *BindError) Unwrap() error { return e.Err }

// Bind decodes the request body into v as JSON and then calls v.Bind(r). The
// body is wrapped in http.MaxBytesReader to bound memory; on overflow Bind
// also sets Connection: close itself, because MaxBytesReader's own close hook
// is a direct type assertion on w that any ResponseWriter wrapper (this
// library's middlewares included) hides, and without it net/http would drain
// up to 256 KiB of the rejected body before reusing the connection.
// json.UnmarshalRead consumes the body to EOF (it rejects trailing data), so
// no explicit drain is needed on success.
func Bind(w http.ResponseWriter, r *http.Request, v Binder, opts ...BindOption) error {
	cfg := bindConfig{maxBytes: DefaultMaxBodyBytes}
	for _, opt := range opts {
		opt(&cfg)
	}
	r.Body = http.MaxBytesReader(w, r.Body, cfg.maxBytes)
	if err := json.UnmarshalRead(r.Body, v); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			w.Header().Set("Connection", "close")
			return &BindError{Status: http.StatusRequestEntityTooLarge, Err: err}
		}
		return &BindError{Status: http.StatusBadRequest, Err: err}
	}
	if err := v.Bind(r); err != nil {
		return &BindError{Status: http.StatusBadRequest, Err: err}
	}
	return nil
}
