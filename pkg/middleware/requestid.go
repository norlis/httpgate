package middleware

import (
	"context"
	"log"
	"net/http"

	"github.com/google/uuid"
)

// Key to use when setting the request ID.
type ctxKeyRequestID int

// RequestIDKey is the key that holds the unique request ID in a request context.
const RequestIDKey ctxKeyRequestID = 0

type RequestIdOption func(*config)

type config struct {
	headerName string
}

func WithHeaderName(name string) RequestIdOption {
	return func(c *config) {
		c.headerName = name
	}
}

// RequestID is a middleware that injects a request ID
func RequestID(opts ...RequestIdOption) func(http.Handler) http.Handler {

	cfg := &config{
		headerName: "X-Request-ID",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(cfg.headerName)

			if requestID == "" {
				id, err := uuid.NewV7()
				if err != nil {
					log.Printf("ERROR: No se pudo generar UUIDv7: %v", err)
					requestID = uuid.NewString()
				} else {
					requestID = id.String()
				}
			}

			w.Header().Set(cfg.headerName, requestID)
			ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetRequestID returns a request ID from the given context if one is present.
// Returns the empty string if a request ID cannot be found.
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if reqID, ok := ctx.Value(RequestIDKey).(string); ok {
		return reqID
	}
	return ""
}
