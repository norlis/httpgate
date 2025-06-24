package presenters

import "net/http"

type Presenters interface {
	JSON(w http.ResponseWriter, r *http.Request, v interface{}, opts ...ResponseOption)
	PlainText(w http.ResponseWriter, r *http.Request, v string, opts ...ResponseOption)
	Error(w http.ResponseWriter, r *http.Request, err error, opts ...ErrorOption)

	Bind(r *http.Request, v Binder) error
	Render(w http.ResponseWriter, r *http.Request, v Renderer) error
}

// Renderer interface for managing response payloads.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request) error
}

// Binder interface for managing request payloads.
type Binder interface {
	Bind(r *http.Request) error
}
