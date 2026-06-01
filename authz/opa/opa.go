// Package opa implements an authz.Enforcer backed by Open Policy Agent,
// evaluating Rego policies loaded from disk. Use Query for constant-query
// introspection and Permissions / AllowedResources for frontend capability hints.
package opa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/norlis/httpgate/authz"
)

// Config configures the OPA client.
type Config struct {
	Query        string   `yaml:"query"`
	PoliciesPath string   `yaml:"policiesPath"`
	DataFiles    []string `yaml:"dataFiles,omitempty"`
}

// Client evaluates authorization policies via the embedded OPA SDK.
type Client struct {
	query        rego.PreparedEvalQuery
	policiesPath string
	dataFiles    []string
	queryCache   sync.Map // map[string]*preparedQuery
	logger       *slog.Logger
}

type preparedQuery struct {
	once  sync.Once
	query rego.PreparedEvalQuery
	err   error
}

// Option configures a Client.
type Option func(*Client)

// WithLogger sets the logger. A nil logger is ignored.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.logger = l
		}
	}
}

// New constructs a Client with the prepared query loaded from cfg.PoliciesPath
// (and any additional cfg.DataFiles).
func New(ctx context.Context, cfg Config, opts ...Option) (*Client, error) {
	if cfg.Query == "" || cfg.PoliciesPath == "" {
		return nil, errors.New("opa: query and policies path cannot be empty")
	}
	c := &Client{
		logger:       slog.New(slog.DiscardHandler),
		policiesPath: cfg.PoliciesPath,
		dataFiles:    append([]string(nil), cfg.DataFiles...),
	}
	for _, opt := range opts {
		opt(c)
	}
	c.logger = c.logger.With(slog.String("logger", "opa"), slog.String("query", cfg.Query))

	prep, err := rego.New(
		rego.Query(cfg.Query),
		rego.Load(append([]string{cfg.PoliciesPath}, cfg.DataFiles...), nil),
	).PrepareForEval(ctx)
	if err != nil {
		c.logger.Error("prepare query", slog.Any("error", err))
		return nil, fmt.Errorf("opa: prepare query: %w", err)
	}
	c.query = prep
	return c, nil
}

// IsAllowed evaluates the policy against the input. Returns false on
// missing/non-bool results.
func (c *Client) IsAllowed(ctx context.Context, in authz.Input) (bool, error) {
	results, err := c.query.Eval(ctx, rego.EvalInput(in))
	if err != nil {
		return false, fmt.Errorf("opa: eval: %w", err)
	}
	if len(results) == 0 {
		return false, nil
	}
	allowed, ok := results[0].Expressions[0].Value.(bool)
	if !ok {
		return false, errors.New("opa: query did not return a boolean")
	}
	return allowed, nil
}

// prepared returns (lazily creating and caching) the PreparedEvalQuery for
// query. A prepare error is permanent for the Client's lifetime: the zero query
// and that error are returned on every subsequent call for the same query.
func (c *Client) prepared(ctx context.Context, query string) (rego.PreparedEvalQuery, error) {
	entry, _ := c.queryCache.LoadOrStore(query, &preparedQuery{})
	pq := entry.(*preparedQuery)
	pq.once.Do(func() {
		prep, err := rego.New(
			rego.Query(query),
			rego.Load(append([]string{c.policiesPath}, c.dataFiles...), nil),
		).PrepareForEval(ctx)
		if err != nil {
			pq.err = fmt.Errorf("opa: prepare %q: %w", query, err)
			return
		}
		pq.query = prep
	})
	return pq.query, pq.err
}

const (
	permissionsQuery      = "data.authz.permissions"
	allowedResourcesQuery = "data.authz.allowed_resources"
)

// Query evaluates a developer-supplied Rego query (a compile-time CONSTANT,
// never user input) against in, returning the decoded value. Prepared queries
// are cached per query string with single-prepare semantics under concurrent
// first access. An undefined query yields (nil, nil).
//
// A prepare failure for a query is permanent for the lifetime of the Client
// (the bundle is loaded once); call New again to recover.
//
// For static data reads, pass a zero Input: Query(ctx, "data.roles", authz.Input{}).
func (c *Client) Query(ctx context.Context, query string, in authz.Input) (any, error) {
	if query == "" {
		return nil, errors.New("opa: query cannot be empty")
	}
	pq, err := c.prepared(ctx, query)
	if err != nil {
		return nil, err
	}
	rs, err := pq.Eval(ctx, rego.EvalInput(in))
	if err != nil {
		return nil, fmt.Errorf("opa: eval %q: %w", query, err)
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		//nolint:nilnil // documented: an undefined query yields (nil, nil), not an error.
		return nil, nil
	}
	return rs[0].Expressions[0].Value, nil
}

// Permissions returns the permission names granted to in's roles by evaluating
// data.authz.permissions. Requires the policy to expose a set rule there.
//
// Roles must come from the caller's authenticated context, never from a
// user-supplied value. This is for UI capability hints (show/hide), not a
// security boundary — IsAllowed still enforces every request.
func (c *Client) Permissions(ctx context.Context, in authz.Input) ([]string, error) {
	v, err := c.Query(ctx, permissionsQuery, in)
	if err != nil {
		return nil, err
	}
	return toStringSlice(v), nil
}

// AllowedResources returns the flattened regex path patterns granted to in's
// roles by evaluating data.authz.allowed_resources. These reveal internal
// route structure; return them only to the authenticated owner of the roles.
func (c *Client) AllowedResources(ctx context.Context, in authz.Input) ([]string, error) {
	v, err := c.Query(ctx, allowedResourcesQuery, in)
	if err != nil {
		return nil, err
	}
	return toStringSlice(v), nil
}

// toStringSlice converts an OPA set (decoded as []any) to []string. A non-slice
// input returns nil; non-string elements are silently dropped.
func toStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
