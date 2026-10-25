package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/norlis/httpgate/authz"
	"github.com/norlis/httpgate/presenter"
)

type authorizeConfig struct {
	logger *slog.Logger
}

// AuthorizeOption configures Authorize.
type AuthorizeOption func(*authorizeConfig)

// WithAuthorizeLogger sets the logger that records enforcer failures (the
// 500 path). Without it those failures are answered but not logged. A nil
// logger is ignored.
func WithAuthorizeLogger(l *slog.Logger) AuthorizeOption {
	return func(c *authorizeConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// Authorize builds middleware that extracts a payload from each request and
// asks the authz.Enforcer whether the action is allowed. All failures are RFC
// 9457: an extraction error is a client error (400, the message is surfaced);
// a denial is 403; an enforcer error is a server fault (500): the raw error is
// logged through WithAuthorizeLogger and never sent to the client, exactly
// like presenter.Error.
//
// The action evaluated is "METHOD:/path?query" (r.URL.RequestURI()).
func Authorize(policyEnforcer authz.Enforcer, extractor authz.PayloadExtractor, opts ...AuthorizeOption) Middleware {
	cfg := &authorizeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload, err := extractor(r)
			if err != nil {
				presenter.Error(w, r, err, presenter.WithStatus(http.StatusBadRequest))
				return
			}

			input := authz.Input{
				Payload: payload,
				Action:  strings.ToUpper(r.Method) + ":" + r.URL.RequestURI(),
			}

			// Agnostic to whether OPA is a service or a library. Any error
			// contacting or evaluating the policy denies access.
			allowed, err := policyEnforcer.IsAllowed(r.Context(), input)
			if err != nil {
				presenter.Error(w, r, err, presenter.WithStatus(http.StatusInternalServerError), presenter.WithLogger(cfg.logger))
				return
			}

			if !allowed {
				presenter.Error(
					w, r, nil,
					presenter.WithStatus(http.StatusForbidden),
					presenter.WithTitle("access denied"),
					presenter.WithDetail("You do not have permission to perform this action."),
				)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
