package presenters

import (
	"encoding/json"
	"io"
	"net/http"
)

// Bind decodes a request body and executes the Binder method of the
// payload structure.
func (p *presenters) Bind(r *http.Request, v Binder) error {
	body := r.Body

	if err := json.NewDecoder(body).Decode(v); err != nil {
		return err
	}
	defer io.Copy(io.Discard, body) //nolint:errcheck

	if err := v.Bind(r); err != nil {
		return err
	}
	return nil
}

func (p *presenters) Render(w http.ResponseWriter, r *http.Request, v Renderer) error {
	if err := v.Render(w, r); err != nil {
		return err
	}
	p.JSON(w, r, v)
	return nil
}
