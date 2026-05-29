package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/norlis/httpgate/authz"
	"github.com/norlis/httpgate/problem"
)

// Authorize builds middleware that extracts a payload from each request and
// asks the authz.Enforcer whether the action is allowed. On extraction error
// it responds 400, on enforcer error 500, and on denial 403 — all as RFC 7807.
func Authorize(policyEnforcer authz.Enforcer, extractor authz.PayloadExtractor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload, err := extractor(r)
			if err != nil {
				problem.Respond(w, problem.FromError(err, http.StatusBadRequest, problem.WithInstance(r)))
				return
			}

			// action = "METHOD:/path"
			// GET:/api/person
			action := fmt.Sprintf("%s:%s", strings.ToUpper(r.Method), r.URL.RequestURI())

			input := authz.Input{
				Payload: payload,
				Action:  action,
			}

			// This call is agnostic to whether OPA is a service or a library.
			allowed, err := policyEnforcer.IsAllowed(r.Context(), input)
			if err != nil {
				// If there's an error contacting or evaluating OPA, it's safer to deny access.
				// Returns 500 Internal Server Error to indicate a system failure.
				problem.Respond(w, problem.FromError(err, http.StatusInternalServerError, problem.WithInstance(r)))
				return
			}

			if !allowed {
				// If the OPA policy returns 'false', deny access.
				// Returns 403 Forbidden, the standard code for an authorization failure.
				p := problem.New(
					"access denied", http.StatusForbidden,
					problem.WithDetail("You do not have permission to perform this action."),
					problem.WithInstance(r),
				)
				problem.Respond(w, p)
				return
			}

			// If the policy allows it, the request continues to the final handler.
			next.ServeHTTP(w, r)
		})
	}
}
