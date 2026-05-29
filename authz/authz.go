// Package authz defines the authorization contract used by httpgate.
//
// Concrete enforcer implementations live in sub-packages
// (e.g. github.com/norlis/httpgate/authz/opa).
package authz

import (
	"context"
	"net/http"
)

// Input is the payload evaluated by an Enforcer for a single request.
type Input struct {
	Payload map[string]any `json:"payload"`
	Action  string         `json:"action"`
}

// Enforcer decides whether a given Input is allowed. Implementations
// must be safe for concurrent use.
type Enforcer interface {
	IsAllowed(ctx context.Context, in Input) (bool, error)
}

// PayloadExtractor turns an HTTP request into the policy payload (claims,
// roles, attributes). It runs on the request hot path; keep it cheap and
// pure where possible.
type PayloadExtractor func(r *http.Request) (map[string]any, error)
