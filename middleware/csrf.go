package middleware

import (
	"fmt"
	"net/http"
)

// csrfConfig holds the options applied to a CSRFProtect instance before
// the underlying http.CrossOriginProtection is constructed.
type csrfConfig struct {
	trustedOrigins []string
	bypassPatterns []string
	denyHandler    http.Handler
}

// CSRFOption configures CSRFProtect.
type CSRFOption func(*csrfConfig)

// WithTrustedOrigin permits requests whose Origin header exactly matches
// the given value. Format: "scheme://host[:port]". No wildcards.
//
// May be called multiple times to add multiple origins.
func WithTrustedOrigin(origin string) CSRFOption {
	return func(c *csrfConfig) {
		c.trustedOrigins = append(c.trustedOrigins, origin)
	}
}

// WithBypassPattern permits all requests whose path matches the
// ServeMux-style pattern. Use sparingly — this disables CSRF protection
// for the matched routes (typical use: webhook endpoints with their own
// signature-based auth).
//
// May be called multiple times to add multiple patterns.
func WithBypassPattern(pattern string) CSRFOption {
	return func(c *csrfConfig) {
		c.bypassPatterns = append(c.bypassPatterns, pattern)
	}
}

// WithDenyHandler overrides the default 403 Forbidden response sent when
// a request is rejected. Use to emit RFC 7807 JSON via presenter.Error or
// to log the rejection.
func WithDenyHandler(h http.Handler) CSRFOption {
	return func(c *csrfConfig) { c.denyHandler = h }
}

// CSRFProtect blocks cross-origin state-changing requests using Go 1.25's
// net/http.CrossOriginProtection (Sec-Fetch-Site header + Origin/Host
// comparison as fallback).
//
// Safe methods (GET, HEAD, OPTIONS) are always allowed. Unsafe methods
// (POST, PUT, DELETE, PATCH) from origins outside the trusted set are
// rejected — by default with 403 Forbidden, or via the handler passed
// to WithDenyHandler.
//
// CSRFProtect is COMPLEMENTARY to CORS, not a replacement. CORS authorizes
// cross-origin READS (the browser is allowed to receive the response);
// CSRFProtect blocks cross-origin WRITES (a malicious page cannot trigger
// state changes using the user's session cookies). A typical secure
// pipeline stacks both:
//
//	chain := middleware.New(
//	    middleware.CORS(corsOpts),          // who can read responses
//	    middleware.CSRFProtect(             // who can write
//	        middleware.WithTrustedOrigin("https://app.example.com"),
//	    ),
//	    middleware.Authorize(enforcer, extract),
//	)
//
// CSRFProtect panics at construction time if any trusted origin string
// is malformed. Misconfiguration of a security primitive must fail fast
// at startup, not silently at runtime.
func CSRFProtect(opts ...CSRFOption) Middleware {
	cfg := &csrfConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	p := http.NewCrossOriginProtection()
	for _, o := range cfg.trustedOrigins {
		if err := p.AddTrustedOrigin(o); err != nil {
			panic(fmt.Errorf("middleware.CSRFProtect: invalid trusted origin %q: %w", o, err))
		}
	}
	for _, pat := range cfg.bypassPatterns {
		p.AddInsecureBypassPattern(pat)
	}
	if cfg.denyHandler != nil {
		p.SetDenyHandler(cfg.denyHandler)
	}
	return p.Handler
}
