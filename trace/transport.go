package trace

import "net/http"

// Transport is an http.RoundTripper that propagates the request context's
// trace as a traceparent header: inherited trace id, local span id as the
// parent for the downstream service. Requests without trace context pass
// through untouched.
type Transport struct {
	// Base performs the round trip; nil means http.DefaultTransport.
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if tc, ok := FromContext(req.Context()); ok {
		// The RoundTripper contract forbids mutating the caller's request.
		req = req.Clone(req.Context())
		req.Header.Set(Header, tc.Traceparent())
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req) //nolint:wrapcheck // pass-through of the wrapped http.RoundTripper's error; wrapping would break the interface contract (e.g. errors.Is/As against the base transport)
}
