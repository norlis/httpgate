package presenters

import "net/http"

// responseConfig es una estructura interna para guardar la configuración de la respuesta.
type responseConfig struct {
	statusCode int
	headers    http.Header
}

// ResponseOption es una función que modifica la configuración de la respuesta.
type ResponseOption func(*responseConfig)

// WithStatusCode es una opción para establecer el código de estado HTTP.
func WithStatusCode(code int) ResponseOption {
	return func(c *responseConfig) {
		c.statusCode = code
	}
}

// WithHeader es una opción para añadir una cabecera a la respuesta.
func WithHeader(key, value string) ResponseOption {
	return func(c *responseConfig) {
		if c.headers == nil {
			c.headers = make(http.Header)
		}
		c.headers.Add(key, value)
	}
}
