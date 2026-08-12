// Package trace implements W3C Trace Context for the platform logging
// standard: traceparent parsing and generation, request-context propagation
// and an outbound http.RoundTripper.
package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// Header is the W3C Trace Context header name, for inbound and outbound use.
const Header = "traceparent"

// ErrMalformed reports a traceparent value that violates the W3C grammar.
var ErrMalformed = errors.New("trace: malformed traceparent")

// Context carries the identifiers of the current trace. It is an immutable
// value type; both fields are lowercase hex validated on construction.
type Context struct {
	TraceID string // 32 hex chars, never all-zero
	SpanID  string // 16 hex chars, never all-zero
}

type ctxKey struct{}

// NewContext returns a copy of ctx carrying tc.
func NewContext(ctx context.Context, tc Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, tc)
}

// FromContext returns the trace Context stored by NewContext, if any.
func FromContext(ctx context.Context) (Context, bool) {
	tc, ok := ctx.Value(ctxKey{}).(Context)
	return tc, ok
}

// New returns a Context with freshly generated random identifiers, for use
// when this service is the trace entrypoint.
func New() Context {
	return Context{TraceID: randomHex(16), SpanID: randomHex(8)}
}

// NewSpanID returns a fresh 64-bit span id as 16 lowercase hex chars, for the
// local work of an inherited trace.
func NewSpanID() string { return randomHex(8) }

// Traceparent renders the W3C header value for outbound propagation, with the
// sampled flag set.
//
// Caveat: the sampled flag is always "01" (this library has no sampling
// concept), and Parse ignores the incoming trace-flags octet entirely — it is
// validated as hex but never inspected or propagated.
func (c Context) Traceparent() string {
	return "00-" + c.TraceID + "-" + c.SpanID + "-01"
}

// baseLen is the length in bytes of a version-00 traceparent header:
// 2 (version) + 1 (-) + 32 (trace-id) + 1 (-) + 16 (parent-id) + 1 (-) + 2 (flags).
const baseLen = 55

// Parse extracts a Context from a traceparent header value, enforcing the W3C
// grammar: version "-" trace-id "-" parent-id "-" trace-flags. Per the spec,
// unknown future versions are accepted when the leading fields match the base
// format; version 00 must match it exactly.
func Parse(traceparent string) (Context, error) {
	if err := validateFrame(traceparent); err != nil {
		return Context{}, err
	}
	traceID := traceparent[3:35]
	spanID := traceparent[36:52]
	flags := traceparent[53:55]
	if !isLowerHex(traceID) || !isLowerHex(spanID) || !isLowerHex(flags) {
		return Context{}, ErrMalformed
	}
	if allZero(traceID) || allZero(spanID) {
		return Context{}, ErrMalformed
	}
	return Context{TraceID: traceID, SpanID: spanID}, nil
}

// validateFrame checks the overall shape of a traceparent value: minimum
// length, the version field, and the fixed-position field separators. It does
// not inspect the trace-id/parent-id/flags contents themselves.
func validateFrame(traceparent string) error {
	if len(traceparent) < baseLen {
		return ErrMalformed
	}
	version := traceparent[0:2]
	if !isLowerHex(version) || version == "ff" {
		return ErrMalformed
	}
	if version == "00" && len(traceparent) != baseLen {
		return ErrMalformed
	}
	if len(traceparent) > baseLen && traceparent[baseLen] != '-' {
		return ErrMalformed
	}
	if traceparent[2] != '-' || traceparent[35] != '-' || traceparent[52] != '-' {
		return ErrMalformed
	}
	return nil
}

func isLowerHex(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func allZero(s string) bool {
	for i := range len(s) {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // documented to never fail since Go 1.24
	return hex.EncodeToString(b)
}
