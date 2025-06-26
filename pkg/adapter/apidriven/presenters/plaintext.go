package presenters

import "net/http"

func (p *presenters) PlainText(w http.ResponseWriter, r *http.Request, v string, opts ...ResponseOption) {
	config := &responseConfig{
		statusCode: http.StatusOK,
		headers:    make(http.Header),
	}
	config.headers.Set("Content-Type", "text/plain; charset=utf-8")

	for _, opt := range opts {
		opt(config)
	}

	for key, values := range config.headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(config.statusCode)
	_, _ = w.Write([]byte(v))
}
