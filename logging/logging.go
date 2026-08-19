// Package logging builds the platform-standard slog logger: NDJSON output,
// OpenTelemetry / ECS field names, ISO 8601 UTC timestamps and automatic
// trace-context injection. See docs/logging.md for the field catalog.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// Field names of the platform logging standard (OTel semantic conventions,
// ECS as fallback). Exported so consumers and tests never repeat literals.
const (
	KeyTimestamp              = "timestamp"
	KeyLevel                  = "log.level"
	KeyMessage                = "message"
	KeyServiceName            = "service.name"
	KeyServiceVersion         = "service.version"
	KeyEnvironment            = "deployment.environment.name"
	KeyTraceID                = "trace_id"
	KeySpanID                 = "span_id"
	KeyErrorType              = "error.type"
	KeyErrorMessage           = "error.message"
	KeyErrorStackTrace        = "error.stack_trace"
	KeyHTTPRequestMethod      = "http.request.method"
	KeyURLPath                = "url.path"
	KeyHTTPResponseStatusCode = "http.response.status_code"
	KeyHTTPResponseBodySize   = "http.response.body.size"
	KeyClientAddress          = "client.address"
	KeyEventDuration          = "event.duration"       // nanoseconds
	KeyEventDurationHuman     = "event.duration_human" // human mirror of KeyEventDuration; never aggregate over it
)

// timeLayout renders ISO 8601 UTC with millisecond precision, per §2.4.
const timeLayout = "2006-01-02T15:04:05.000Z"

type config struct {
	service     string
	version     string
	environment string
	level       slog.Level
}

// Option configures New.
type Option func(*config)

// WithService sets service.name and service.version on every log line.
func WithService(name, version string) Option {
	return func(c *config) { c.service, c.version = name, version }
}

// WithEnvironment sets deployment.environment.name on every log line.
func WithEnvironment(env string) Option {
	return func(c *config) { c.environment = env }
}

// WithLevel sets the minimum level. The default is Info: DEBUG stays off
// unless a service asks for it explicitly (§2.5).
func WithLevel(l slog.Level) Option {
	return func(c *config) { c.level = l }
}

// New returns the platform-standard logger writing NDJSON to w. Use the
// *Context logger methods so trace_id and span_id are injected automatically.
func New(w io.Writer, opts ...Option) *slog.Logger {
	cfg := config{level: slog.LevelInfo}
	for _, opt := range opts {
		opt(&cfg)
	}

	var inner slog.Handler = slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       cfg.level,
		ReplaceAttr: replaceAttr,
	})

	// Base fields are attached once here, not per log line.
	base := make([]slog.Attr, 0, 3)
	if cfg.service != "" {
		base = append(base, slog.String(KeyServiceName, cfg.service))
	}
	if cfg.version != "" {
		base = append(base, slog.String(KeyServiceVersion, cfg.version))
	}
	if cfg.environment != "" {
		base = append(base, slog.String(KeyEnvironment, cfg.environment))
	}
	if len(base) > 0 {
		inner = inner.WithAttrs(base)
	}
	return slog.New(&handler{inner: inner})
}

// replaceAttr maps slog's built-in top-level keys to the standard's names;
// attributes inside groups are left untouched.
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		if a.Value.Kind() != slog.KindTime {
			return a
		}
		return slog.String(KeyTimestamp, a.Value.Time().UTC().Format(timeLayout))
	case slog.LevelKey:
		level, ok := a.Value.Any().(slog.Level)
		if !ok {
			return a
		}
		return slog.String(KeyLevel, strings.ToLower(level.String()))
	case slog.MessageKey:
		if a.Value.Kind() != slog.KindString {
			return a
		}
		a.Key = KeyMessage
		return a
	}
	return a
}
