package presenter

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"

	"github.com/norlis/httpgate/logging"
)

// JSON marshals v to JSON and writes it to w. Defaults: 200 OK,
// Content-Type application/json. Use ResponseOptions to override.
// Marshals into memory first so an encode failure yields a clean 500 with no
// body instead of a 200 with a truncated one; map keys are sorted so equal
// values produce equal bytes (ETags, snapshot tests).
func JSON(w http.ResponseWriter, r *http.Request, v any, opts ...ResponseOption) {
	body, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		slog.Default().ErrorContext(r.Context(), "json encoding failed", logging.Err(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	cfg := &responseConfig{statusCode: http.StatusOK, headers: make(http.Header)}
	cfg.headers.Set("Content-Type", "application/json")
	for _, opt := range opts {
		opt(cfg)
	}
	for k, vs := range cfg.headers {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(cfg.statusCode)
	if _, err := w.Write(body); err != nil {
		slog.Default().ErrorContext(r.Context(), "json write failed", logging.Err(err))
	}
}
