package presenter

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
)

// Binder is implemented by request DTOs that decode themselves from r.
type Binder interface {
	Bind(r *http.Request) error
}

// Renderer is implemented by response DTOs that render themselves to w.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request) error
}

// Bind decodes the request body into v as JSON and then calls v.Bind(r).
func Bind(r *http.Request, v Binder) error {
	body := r.Body
	if err := json.UnmarshalRead(body, v); err != nil {
		return fmt.Errorf("presenter: decode body: %w", err)
	}
	defer io.Copy(io.Discard, body) //nolint:errcheck
	if err := v.Bind(r); err != nil {
		return fmt.Errorf("presenter: bind: %w", err)
	}
	return nil
}

// Render calls v.Render and then serializes v as JSON.
func Render(w http.ResponseWriter, r *http.Request, v Renderer) error {
	if err := v.Render(w, r); err != nil {
		return fmt.Errorf("presenter: render: %w", err)
	}
	JSON(w, r, v)
	return nil
}
