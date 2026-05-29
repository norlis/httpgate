// Package opa implements an authz.Enforcer backed by Open Policy Agent,
// evaluating Rego policies loaded from disk, plus declarative-data lookups.
package opa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	dataCache    sync.Map // map[string]*dataQuery
	logger       *slog.Logger
}

type dataQuery struct {
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

// Data evaluates data.<path> against the loaded bundle and returns the
// decoded value. path is a dot-separated identifier without the "data."
// prefix; nested paths work (e.g. "roles.admin").
//
// Undefined paths return (nil, nil), matching OPA semantics.
//
// Prepared queries are cached per path with single-prepare semantics
// under concurrent first-access (sync.Once).
//
// Data is for declarative data only. To evaluate policy decisions, use
// IsAllowed.
func (c *Client) Data(ctx context.Context, path string) (any, error) {
	if path == "" {
		return nil, errors.New("opa: data path cannot be empty")
	}
	if strings.HasPrefix(path, "data.") {
		return nil, fmt.Errorf("opa: data path must not include 'data.' prefix: %q", path)
	}

	entry, _ := c.dataCache.LoadOrStore(path, &dataQuery{})
	dq := entry.(*dataQuery)
	dq.once.Do(func() {
		// Build the query using bracket notation so segments that happen
		// to collide with Rego keywords (e.g. "not", "if", "in") parse
		// cleanly: data["foo"]["bar"].
		var b strings.Builder
		b.WriteString("data")
		for seg := range strings.SplitSeq(path, ".") {
			b.WriteString("[\"")
			b.WriteString(seg)
			b.WriteString("\"]")
		}
		prep, err := rego.New(
			rego.Query(b.String()),
			rego.Load(append([]string{c.policiesPath}, c.dataFiles...), nil),
		).PrepareForEval(ctx)
		if err != nil {
			dq.err = fmt.Errorf("opa: prepare data %q: %w", path, err)
			return
		}
		dq.query = prep
	})
	if dq.err != nil {
		return nil, dq.err
	}
	results, err := dq.query.Eval(ctx)
	if err != nil {
		return nil, fmt.Errorf("opa: eval data %q: %w", path, err)
	}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		//nolint:nilnil // documented contract: an unknown/empty OPA path yields (nil, nil), not an error.
		return nil, nil
	}
	return results[0].Expressions[0].Value, nil
}
