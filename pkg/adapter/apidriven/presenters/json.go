package presenters

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// JSON marshals 'v' to JSON, automatically escaping HTML
// Permite pasar opcionalmente statusCode y headers adicionales.
func (p *presenters) JSON(w http.ResponseWriter, r *http.Request, v interface{}, opts ...ResponseOption) {
	config := &responseConfig{
		statusCode: http.StatusOK,
		headers:    make(http.Header),
	}
	config.headers.Set("Content-Type", "application/json")

	for _, opt := range opts {
		opt(config)
	}

	for key, values := range config.headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(config.statusCode)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		p.log.Error("failed to encode json response", zap.Error(err))
	}
}
