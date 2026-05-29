// Package presenter writes HTTP responses: JSON, plain text and RFC 7807
// problem+json errors, all via small free functions configured with options.
package presenter

import (
	"log/slog"
	"net/http"

	"github.com/norlis/httpgate/problem"
)

type errorConfig struct {
	status int
	title  string
	detail string
	logger *slog.Logger
}

// ErrorOption configures an error response written by Error.
type ErrorOption func(*errorConfig)

// WithStatus sets the HTTP status code (default 500).
func WithStatus(status int) ErrorOption { return func(c *errorConfig) { c.status = status } }

// WithTitle sets the problem title (default: http.StatusText of the status).
func WithTitle(title string) ErrorOption { return func(c *errorConfig) { c.title = title } }

// WithDetail sets the problem detail (default: the error's message).
func WithDetail(detail string) ErrorOption { return func(c *errorConfig) { c.detail = detail } }

// WithLogger sets the logger used to record 5xx responses. A nil logger is ignored.
func WithLogger(l *slog.Logger) ErrorOption {
	return func(c *errorConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// Error writes an RFC 7807 problem+json error response. Defaults: 500 with
// the http.StatusText title. If the status is 5xx and a logger has been
// provided via WithLogger, the error is logged at Error level.
func Error(w http.ResponseWriter, r *http.Request, err error, opts ...ErrorOption) {
	cfg := &errorConfig{status: http.StatusInternalServerError}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.title == "" {
		cfg.title = http.StatusText(cfg.status)
	}
	if cfg.detail == "" && err != nil {
		cfg.detail = err.Error()
	}
	if cfg.detail == "" {
		cfg.detail = "unknown error"
	}
	if cfg.status >= 500 && cfg.logger != nil {
		cfg.logger.Error(
			"server error",
			slog.Any("error", err),
			slog.Int("status", cfg.status),
			slog.String("detail", cfg.detail),
		)
	}
	pd := problem.New(
		cfg.title, cfg.status,
		problem.WithDetail(cfg.detail),
		problem.WithInstance(r),
	)
	problem.Respond(w, pd)
}
