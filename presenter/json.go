package presenter

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// JSON marshals v to JSON and writes it to w. Defaults: 200 OK,
// Content-Type application/json. Use ResponseOptions to override.
func JSON(w http.ResponseWriter, r *http.Request, v any, opts ...ResponseOption) {
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
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Error("presenter: encode json", slog.Any("error", err))
	}
}
