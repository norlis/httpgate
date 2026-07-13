package main

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/norlis/httpgate/authz"
	"github.com/norlis/httpgate/authz/opa"
	"github.com/norlis/httpgate/presenter"
)

// permissionsHandler returns the caller's capability set — permission names and
// allowed resource patterns — for frontend menu visibility. Roles come from the
// X-Roles header (a JWT stand-in); this is a UX aid, not a security boundary:
// Authorize still enforces every /api/ request.
func permissionsHandler(enforcer *opa.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, err := extractRoles(r)
		if err != nil {
			presenter.Error(w, r, err, presenter.WithStatus(http.StatusBadRequest))
			return
		}
		in := authz.Input{Payload: payload}

		perms, err := enforcer.Permissions(r.Context(), in)
		if err != nil {
			presenter.Error(w, r, err, presenter.WithLogger(logger))
			return
		}
		resources, err := enforcer.AllowedResources(r.Context(), in)
		if err != nil {
			presenter.Error(w, r, err, presenter.WithLogger(logger))
			return
		}

		presenter.JSON(w, r, map[string]any{
			"roles":       payload["roles"],
			"permissions": perms,
			"resources":   resources,
		})
	}
}

// helloHandler writes a 202 JSON greeting with a custom header.
func helloHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presenter.JSON(
			w, r,
			map[string]string{"text": "Hello World"},
			presenter.WithStatusCode(http.StatusAccepted),
			presenter.WithHeader("x-test", "1"),
		)
	}
}

// errorHandler always fails with a 400 problem+json.
func errorHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presenter.Error(
			w, r, errors.New("error occurred"),
			presenter.WithStatus(http.StatusBadRequest),
		)
	}
}

// panicHandler panics, exercising middleware.Recover.
func panicHandler() http.HandlerFunc {
	return func(_ http.ResponseWriter, _ *http.Request) {
		panic(errors.New("panic test"))
	}
}

// extractRoles reads `X-Roles` (CSV) for demo purposes. In production, parse a
// JWT or session cookie here.
func extractRoles(r *http.Request) (map[string]any, error) {
	raw := r.Header.Get("X-Roles")
	var roles []string
	if raw != "" {
		for p := range strings.SplitSeq(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				roles = append(roles, p)
			}
		}
	}
	return map[string]any{"roles": roles}, nil
}

// Compile-time check: PayloadExtractor signature.
var _ authz.PayloadExtractor = extractRoles
