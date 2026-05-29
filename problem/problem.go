// Package problem implements RFC 7807 (Problem Details for HTTP APIs).
// https://tools.ietf.org/html/rfc7807
package problem

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Detail is the RFC 7807 representation of a single problem occurrence.
// It satisfies the error interface so it can flow through standard error
// handling.
//
//nolint:errname // RFC 7807 names this type "Problem Detail"; Detail is intentional, not an XxxError.
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

	// StackTrace optionally carries debugging information. Should not be
	// populated in production responses.
	StackTrace string `json:"stackTrace,omitempty"`
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

// WithInstance fills Instance from r.URL.Path and (if present) RequestID
// from the X-Request-Id header.
func WithInstance(r *http.Request) Option {
	return func(d *Detail) {
		if r == nil {
			return
		}
		d.Instance = r.URL.Path
		if id := r.Header.Get("X-Request-Id"); id != "" {
			d.RequestID = id
		}
	}
}

// Respond serializes d as application/problem+json and writes it.
func Respond(w http.ResponseWriter, d *Detail) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(d.Status)
	if err := json.NewEncoder(w).Encode(d); err != nil {
		slog.Default().Error("problem: encode detail", slog.Any("error", err))
	}
}
