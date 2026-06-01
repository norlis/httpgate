package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/norlis/httpgate/problem"
)

type traceIDConfig struct {
	headerName string
	logger     *slog.Logger
}

// TraceIDOption configures the TraceID middleware.
type TraceIDOption func(*traceIDConfig)

// WithHeaderName sets the request/response header carrying the trace ID.
// Defaults to "Transactionid".
func WithHeaderName(name string) TraceIDOption {
	return func(c *traceIDConfig) {
		c.headerName = http.CanonicalHeaderKey(name)
	}
}

// WithLogger sets the logger used to report trace-ID generation failures.
func WithLogger(l *slog.Logger) TraceIDOption {
	return func(c *traceIDConfig) {
		c.logger = l
	}
}

// TraceID builds middleware that ensures every request carries a valid UUID
// trace ID: it reuses a valid incoming one or generates a UUIDv7, stores it in
// the context, and echoes it back in the response header.
func TraceID(opts ...TraceIDOption) func(http.Handler) http.Handler {
	cfg := &traceIDConfig{
		headerName: http.CanonicalHeaderKey("TransactionId"),
		logger:     slog.New(slog.DiscardHandler),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := r.Header.Get(cfg.headerName)

			if _, err := uuid.Parse(traceID); err != nil {
				newID, V7Err := uuid.NewV7()
				if V7Err != nil {
					// Fallback to V4 if NewV7 fails (extremely rare).
					cfg.logger.Error("Failed to generate UUIDv7, falling back to v4", slog.Any("error", V7Err))
					traceID = uuid.NewString()
				} else {
					traceID = newID.String()
				}
			}

			ctx := problem.ContextWithRequestID(r.Context(), traceID)
			w.Header().Set(cfg.headerName, traceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TraceIDFromContext returns the trace ID stored by TraceID, or "" if absent.
// It delegates to problem.RequestIDFromContext so the trace ID and the
// problem+json RequestID share a single context key.
func TraceIDFromContext(ctx context.Context) string {
	return problem.RequestIDFromContext(ctx)
}
