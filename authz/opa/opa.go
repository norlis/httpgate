// Package opa implements an authz.Enforcer backed by Open Policy Agent,
// evaluating Rego policies loaded from disk. Use Query for constant-query
// introspection and Permissions / AllowedResources for frontend capability hints.
package opa

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/loader"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmem"

	"github.com/norlis/httpgate/authz"
)

// Config configures the OPA client.
type Config struct {
	Query        string   `yaml:"query"`
	PoliciesPath string   `yaml:"policiesPath"`
	DataFiles    []string `yaml:"dataFiles,omitempty"`
}

// Client evaluates authorization policies via the embedded OPA SDK.
// All compiled state lives in one immutable *prepared behind an atomic
// pointer so readers never lock; only writers (lazy prepare, Reload) take mu.
type Client struct {
	policiesPath string
	dataFiles    []string
	mainQuery    string
	logger       *slog.Logger

	dataMode bool           // WithData was given: in-memory store, Reload enabled
	seed     map[string]any // initial document; cleared after New
	modules  []*ast.Module  // .rego under PoliciesPath, parsed once (data mode only)
	mu       sync.Mutex     // serializes writers of state
	state    atomic.Pointer[prepared]
}

var (
	errBothDataSources = errors.New("opa: DataFiles and WithData are mutually exclusive")
	errDataFilesMode   = errors.New("opa: Reload requires a client created with WithData")
	errNilData         = errors.New("opa: data document cannot be nil")
)

// prepared is an immutable snapshot of compiled queries over one store.
type prepared struct {
	store   storage.Store
	allow   rego.PreparedEvalQuery
	queries map[string]rego.PreparedEvalQuery
}

// Option configures a Client.
type Option func(*Client)

// WithData seeds the OPA data document (data.*) from memory instead of files.
// Mutually exclusive with Config.DataFiles. Enables Reload. A nil map is an
// error at New: it must never fall back to data files on disk.
func WithData(data map[string]any) Option {
	return func(c *Client) {
		c.dataMode = true
		c.seed = data
	}
}

// newStore builds an in-memory store from data, returning (not panicking on)
// values that cannot be represented as JSON.
func newStore(ctx context.Context, data map[string]any) (storage.Store, error) {
	store := inmem.NewWithOpts(inmem.OptRoundTripOnWrite(true))
	txn, err := store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return nil, fmt.Errorf("store txn: %w", err)
	}
	if err := store.Write(ctx, txn, storage.AddOp, storage.Path{}, data); err != nil {
		store.Abort(ctx, txn)
		return nil, fmt.Errorf("store write: %w", err)
	}
	if err := store.Commit(ctx, txn); err != nil {
		return nil, fmt.Errorf("store commit: %w", err)
	}
	return store, nil
}

// onlyRegoFiles skips every non-module file so data under PoliciesPath never
// merges with the in-memory document.
func onlyRegoFiles(_ string, info fs.FileInfo, _ int) bool {
	return !info.IsDir() && !strings.HasSuffix(info.Name(), ".rego")
}

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
		mainQuery:    cfg.Query,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.logger = c.logger.With(slog.String("query", cfg.Query))
	if c.dataMode && len(c.dataFiles) > 0 {
		return nil, errBothDataSources
	}
	var store storage.Store
	if c.dataMode {
		if c.seed == nil {
			return nil, errNilData
		}
		// Modules are parsed once here; file-mode re-reads disk per prepare,
		// which rego.Load cannot do against a caller-provided store.
		res, err := loader.NewFileLoader().Filtered([]string{c.policiesPath}, onlyRegoFiles)
		if err != nil {
			return nil, fmt.Errorf("opa: load policies: %w", err)
		}
		for _, name := range slices.Sorted(maps.Keys(res.Modules)) {
			c.modules = append(c.modules, res.Modules[name].Parsed)
		}
		store, err = newStore(ctx, c.seed)
		if err != nil {
			return nil, fmt.Errorf("opa: %w", err)
		}
		c.seed = nil // the store holds the live document from here on
	}

	p, err := c.build(ctx, store, nil)
	if err != nil {
		// Not logged here: the wrapped error is returned to the caller, who
		// logs it once (log-and-rethrow is forbidden by the logging standard).
		return nil, fmt.Errorf("opa: prepare query: %w", err)
	}
	c.state.Store(p)
	return c, nil
}

// build compiles the main query plus every query name in extra over store.
// A nil store means "load data from PoliciesPath + DataFiles" (file mode).
func (c *Client) build(ctx context.Context, store storage.Store, extra []string) (*prepared, error) {
	p := &prepared{store: store, queries: make(map[string]rego.PreparedEvalQuery, len(extra))}
	allow, err := c.prepare(ctx, store, c.mainQuery)
	if err != nil {
		return nil, err
	}
	p.allow = allow
	for _, q := range extra {
		pq, err := c.prepare(ctx, store, q)
		if err != nil {
			return nil, err
		}
		p.queries[q] = pq
	}
	return p, nil
}

// prepare compiles one query. File mode loads modules + data from disk; data
// mode pairs the pre-parsed modules with the given store.
func (c *Client) prepare(ctx context.Context, store storage.Store, query string) (rego.PreparedEvalQuery, error) {
	opts := []func(*rego.Rego){rego.Query(query)}
	if store == nil {
		opts = append(opts, rego.Load(append([]string{c.policiesPath}, c.dataFiles...), nil))
	} else {
		opts = append(opts, rego.Store(store))
		for _, m := range c.modules {
			opts = append(opts, rego.ParsedModule(m))
		}
	}
	pq, err := rego.New(opts...).PrepareForEval(ctx)
	if err != nil {
		return rego.PreparedEvalQuery{}, fmt.Errorf("prepare %q: %w", query, err)
	}
	return pq, nil
}

// IsAllowed evaluates the policy against the input. Returns false on
// missing/non-bool results.
func (c *Client) IsAllowed(ctx context.Context, in authz.Input) (bool, error) {
	results, err := c.state.Load().allow.Eval(ctx, rego.EvalInput(in))
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

// prepared returns the PreparedEvalQuery for query, compiling it on first use
// against the current store. Copy-on-write under mu so a concurrent Reload
// cannot drop a query prepared in between.
func (c *Client) prepared(ctx context.Context, query string) (rego.PreparedEvalQuery, error) {
	if pq, ok := c.state.Load().queries[query]; ok {
		return pq, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := c.state.Load()
	if pq, ok := cur.queries[query]; ok { // lost the race to another writer
		return pq, nil
	}
	pq, err := c.prepare(ctx, cur.store, query)
	if err != nil {
		return rego.PreparedEvalQuery{}, fmt.Errorf("opa: %w", err)
	}
	next := &prepared{store: cur.store, allow: cur.allow, queries: maps.Clone(cur.queries)}
	next.queries[query] = pq
	c.state.Store(next)
	return pq, nil
}

// Reload atomically replaces the data document and re-prepares the main query
// plus every query compiled so far. On error the previous state stays in
// service. Only valid for clients created with WithData.
func (c *Client) Reload(ctx context.Context, data map[string]any) error {
	if !c.dataMode {
		return errDataFilesMode
	}
	if data == nil {
		return errNilData
	}
	store, err := newStore(ctx, data)
	if err != nil {
		return fmt.Errorf("opa: reload: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := c.state.Load()
	p, err := c.build(ctx, store, slices.Sorted(maps.Keys(cur.queries)))
	if err != nil {
		return fmt.Errorf("opa: reload: %w", err)
	}
	c.state.Store(p)
	c.logger.DebugContext(ctx, "opa data reloaded", slog.Int("queries", len(p.queries)))
	return nil
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
// A prepare failure is returned to the caller and retried on the next call.
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
