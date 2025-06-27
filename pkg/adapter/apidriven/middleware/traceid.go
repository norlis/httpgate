package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type ctxTraceKey struct{}

type traceIdConfig struct {
	headerName string
	logger     *zap.Logger
}

type TraceIdOption func(*traceIdConfig)

func WithHeaderName(name string) TraceIdOption {
	return func(c *traceIdConfig) {
		c.headerName = http.CanonicalHeaderKey(name)
	}
}

func WithLogger(l *zap.Logger) TraceIdOption {
	return func(c *traceIdConfig) {
		c.logger = l
	}
}

func TraceId(opts ...TraceIdOption) func(http.Handler) http.Handler {
	cfg := &traceIdConfig{
		headerName: http.CanonicalHeaderKey("TransactionId"),
		logger:     zap.NewNop(),
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
					// Fallback a V4 si NewV7 falla (extremadamente raro).
					cfg.logger.Error("Failed to generate UUIDv7, falling back to v4", zap.Error(V7Err))
					traceID = uuid.NewString()
				} else {
					traceID = newID.String()
				}
			}

			ctx := context.WithValue(r.Context(), ctxTraceKey{}, traceID)
			w.Header().Set(cfg.headerName, traceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func TraceIdFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(ctxTraceKey{}).(string); ok {
		return id
	}
	return ""
}

// RequestID | GetRequestID alias for old versions
var RequestID = TraceId
var GetRequestID = TraceIdFromContext
