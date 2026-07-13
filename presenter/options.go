package presenter

import "net/http"

// responseConfig is the internal config a ResponseOption mutates.
type responseConfig struct {
	statusCode int
	headers    http.Header
}

// ResponseOption configures a JSON or PlainText response.
type ResponseOption func(*responseConfig)

// WithStatusCode sets the HTTP status code on the response.
func WithStatusCode(code int) ResponseOption {
	return func(c *responseConfig) {
		c.statusCode = code
	}
}

// WithHeader appends a header to the response.
func WithHeader(key, value string) ResponseOption {
	return func(c *responseConfig) {
		if c.headers == nil {
			c.headers = make(http.Header)
		}
		c.headers.Add(key, value)
	}
}
