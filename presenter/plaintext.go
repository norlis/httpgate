package presenter

import "net/http"

// PlainText writes s as text/plain. Defaults: 200 OK. Use ResponseOptions
// to override.
func PlainText(w http.ResponseWriter, r *http.Request, s string, opts ...ResponseOption) {
	cfg := &responseConfig{statusCode: http.StatusOK, headers: make(http.Header)}
	cfg.headers.Set("Content-Type", "text/plain; charset=utf-8")
	for _, opt := range opts {
		opt(cfg)
	}
	for k, vs := range cfg.headers {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(cfg.statusCode)
	_, _ = w.Write([]byte(s))
}
