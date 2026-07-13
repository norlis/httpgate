package opa

import (
	"slices"
	"sync"
	"testing"

	"github.com/norlis/httpgate/authz"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(t.Context(), Config{
		Query:        "data.authz.allow",
		PoliciesPath: "../../policies/authz",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestQuery_staticDataReturnsMap(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	got, err := c.Query(t.Context(), "data.roles", authz.Input{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("roles should decode as map, got %T", got)
	}
}

func TestQuery_undefinedReturnsNilNoError(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	got, err := c.Query(t.Context(), "data.does_not_exist", authz.Input{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

func TestQuery_emptyQueryReturnsError(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	if _, err := c.Query(t.Context(), "", authz.Input{}); err == nil {
		t.Fatal("empty query: expected error, got nil")
	}
}

func TestQuery_inputDriven(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	// whitelist.json contains "GET:/health$"; authz.allow is true for a matching action.
	yes, err := c.Query(t.Context(), "data.authz.allow",
		authz.Input{Action: "GET:/health"})
	if err != nil {
		t.Fatalf("Query allow (match): %v", err)
	}
	if b, ok := yes.(bool); !ok || !b {
		t.Fatalf("allow for GET:/health = %v (%T), want bool true", yes, yes)
	}
	no, err := c.Query(t.Context(), "data.authz.allow",
		authz.Input{Action: "DELETE:/nope"})
	if err != nil {
		t.Fatalf("Query allow (no match): %v", err)
	}
	if b, ok := no.(bool); ok && b {
		t.Fatalf("allow for DELETE:/nope = %v, want false/undefined", no)
	}
}

func TestQuery_cachesPreparedQuery(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	ctx := t.Context()
	if _, err := c.Query(ctx, "data.roles", authz.Input{}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, ok := c.queryCache.Load("data.roles"); !ok {
		t.Fatal("cache miss after first call")
	}
	// Second call must reuse the cached prepared query and still succeed.
	if _, err := c.Query(ctx, "data.roles", authz.Input{}); err != nil {
		t.Fatalf("second call (cached): %v", err)
	}
}

func sortedEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	slices.Sort(g)
	slices.Sort(w)
	return slices.Equal(g, w)
}

func adminInput() authz.Input {
	return authz.Input{Payload: map[string]any{"roles": []string{"admin"}}}
}

func TestPermissions_returnsRolePermissions(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	got, err := c.Permissions(t.Context(), adminInput())
	if err != nil {
		t.Fatalf("Permissions: %v", err)
	}
	if !sortedEqual(got, []string{"super-admin"}) {
		t.Fatalf("got %v, want [super-admin]", got)
	}
}

func TestPermissions_anonymousWhenNoRoles(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	got, err := c.Permissions(t.Context(), authz.Input{})
	if err != nil {
		t.Fatalf("Permissions: %v", err)
	}
	if !sortedEqual(got, []string{"whitelist"}) {
		t.Fatalf("got %v, want [whitelist]", got)
	}
}

func TestAllowedResources_returnsRolePatterns(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	got, err := c.AllowedResources(t.Context(), adminInput())
	if err != nil {
		t.Fatalf("AllowedResources: %v", err)
	}
	if !sortedEqual(got, []string{"(GET|POST|PUT|DELETE|HEAD):/.*"}) {
		t.Fatalf("got %v, want the super-admin pattern", got)
	}
}

func TestAllowedResources_anonymousWhenNoRoles(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	got, err := c.AllowedResources(t.Context(), authz.Input{})
	if err != nil {
		t.Fatalf("AllowedResources: %v", err)
	}
	want := []string{
		"GET:/status", "GET:/live$", "GET:/ready$",
		"GET:/api/test$", "GET:/api/test-err$", "GET:/api/panic",
	}
	if !sortedEqual(got, want) {
		t.Fatalf("got %v, want the whitelist patterns %v", got, want)
	}
}

func TestQuery_concurrentSinglePrepare(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	ctx := t.Context()
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for range 50 {
		wg.Go(func() {
			if _, err := c.Query(ctx, "data.permissions", authz.Input{}); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent call: %v", e)
	}
	var n int
	c.queryCache.Range(func(k, _ any) bool {
		if k == "data.permissions" {
			n++
		}
		return true
	})
	if n != 1 {
		t.Fatalf("queryCache entries for key = %d, want 1", n)
	}
}
