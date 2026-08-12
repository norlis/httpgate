// Package problem implements RFC 9457 (Problem Details for HTTP APIs), which
// obsoletes RFC 7807. The wire format (application/problem+json) is unchanged;
// when "type" is omitted it defaults to "about:blank" per the spec.
// https://www.rfc-editor.org/rfc/rfc9457
package problem

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/norlis/httpgate/logging"
)

// Detail is the RFC 9457 representation of a single problem occurrence.
// It satisfies the error interface so it can flow through standard error
// handling.
//
//nolint:errname // RFC 9457 names this type "Problem Detail"; Detail is intentional, not an XxxError.
type Detail struct {
	// Type is a URI identifying the problem type. Should provide
	// developer-readable documentation.
	Type string `json:"type,omitempty"`

	// Title is a short, human-readable summary of the problem type.
	// Should not change across occurrences of the same problem.
	Title string `json:"title"`

	// Status is the HTTP status code for this occurrence.
	Status int `json:"status"`

	// Detail is a human-readable explanation specific to this occurrence.
	Detail string `json:"detail,omitempty"`

	// Instance is a URI identifying the specific occurrence of the problem.
	Instance string `json:"instance,omitempty"`

	// RequestID correlates the problem to a specific request.
	RequestID string `json:"requestId,omitempty"`

	// Timestamp records when the problem was created.
	Timestamp time.Time `json:"timestamp,omitzero"`
}

func (d *Detail) Error() string { return d.Title }

// Option configures a Detail.
type Option func(*Detail)

// New constructs a Detail with the given title and HTTP status. Timestamp
// is set to time.Now().UTC().
func New(title string, status int, opts ...Option) *Detail {
	d := &Detail{Title: title, Status: status, Timestamp: time.Now().UTC()}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// FromError constructs a Detail whose title is the standard text for the
// status code and whose detail is err.Error() (unless overridden by an
// option).
func FromError(err error, status int, opts ...Option) *Detail {
	d := New(http.StatusText(status), status, opts...)
	if d.Detail == "" && err != nil {
		d.Detail = err.Error()
	}
	return d
}

// WithType sets the Type URI.
func WithType(uri string) Option { return func(d *Detail) { d.Type = uri } }

// WithDetail sets the Detail explanation.
func WithDetail(detail string) Option { return func(d *Detail) { d.Detail = detail } }

// WithRequestID sets the RequestID directly. Use this when the ID lives in
// the request context rather than in the X-Request-Id header.
func WithRequestID(id string) Option { return func(d *Detail) { d.RequestID = id } }

// WithInstance fills Instance from r.URL.Path and (if present) RequestID from
// the request ID stored in the request context (see ContextWithRequestID).
// This keeps the RequestID aligned with the correlation/trace ID set by
// middleware, instead of guessing a header name.
func WithInstance(r *http.Request) Option {
	return func(d *Detail) {
		if r == nil {
			return
		}
		d.Instance = r.URL.Path
		if id := RequestIDFromContext(r.Context()); id != "" {
			d.RequestID = id
		}
	}
}

type ctxRequestIDKey struct{}

// ContextWithRequestID returns a copy of ctx carrying the request/correlation
// ID. Middleware (e.g. TraceContext) stores the ID here so problem responses and
// logs share one identifier without coupling to a specific header.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestIDKey{}, id)
}

// RequestIDFromContext returns the request ID stored by ContextWithRequestID,
// or "" if absent.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(ctxRequestIDKey{}).(string)
	return id
}

// WithRequestIDFromContext sets RequestID from the ID stored in ctx. It is a
// no-op when ctx carries no request ID.
func WithRequestIDFromContext(ctx context.Context) Option {
	return func(d *Detail) {
		if id := RequestIDFromContext(ctx); id != "" {
			d.RequestID = id
		}
	}
}

// Respond serializes d as application/problem+json and writes it.
func Respond(w http.ResponseWriter, d *Detail) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(d.Status)
	if err := json.NewEncoder(w).Encode(d); err != nil {
		slog.Default().Error("problem encoding failed", logging.Err(err))
	}
}
