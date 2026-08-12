// Package presenter writes HTTP responses: JSON, plain text and RFC 9457
// problem+json errors, all via small free functions configured with options.
package presenter

import (
	"log/slog"
	"net/http"

	"github.com/norlis/httpgate/logging"
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

// Error writes an RFC 9457 problem+json error response. Defaults: 500 with
// the http.StatusText title. Server faults (5xx) never surface the raw error
// to the client — it is logged instead (when WithLogger is set); client
// errors (4xx) surface the message, which is safe and useful.
func Error(w http.ResponseWriter, r *http.Request, err error, opts ...ErrorOption) {
	cfg := &errorConfig{status: http.StatusInternalServerError}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.title == "" {
		cfg.title = http.StatusText(cfg.status)
	}

	if cfg.status >= 500 {
		logServerError(r, cfg, err)
	} else {
		cfg.detail = clientDetail(cfg.detail, err)
	}

	pd := problem.New(
		cfg.title, cfg.status,
		problem.WithDetail(cfg.detail),
		problem.WithInstance(r),
	)
	problem.Respond(w, pd)
}

// logServerError logs a 5xx fault once, as the standard structured error
// object. The raw error is deliberately kept out of the client response.
func logServerError(r *http.Request, cfg *errorConfig, err error) {
	if cfg.logger == nil {
		return
	}
	cfg.logger.ErrorContext(
		r.Context(),
		"server error",
		logging.Err(err),
		slog.Int(logging.KeyHTTPResponseStatusCode, cfg.status),
	)
}

// clientDetail returns the detail to surface for a client (4xx) error: an
// explicit detail wins, then the error message, then a generic fallback.
func clientDetail(detail string, err error) string {
	switch {
	case detail != "":
		return detail
	case err != nil:
		return err.Error()
	default:
		return "unknown error"
	}
}
