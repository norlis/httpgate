package opa

import (
	"strings"
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

func TestData_returnsKnownStructure(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	got, err := c.Data(t.Context(), "roles")
	if err != nil {
		t.Fatalf("Data: %v", err)
	}
	if got == nil {
		t.Fatal("Data returned nil for known path 'roles'")
	}
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("roles should decode as map, got %T", got)
	}
}

func TestData_unknownPathReturnsNilNoError(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	got, err := c.Data(t.Context(), "does.not.exist")
	if err != nil {
		t.Fatalf("Data: unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

func TestData_emptyPathReturnsError(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	_, err := c.Data(t.Context(), "")
	if err == nil {
		t.Fatal("empty path: expected error, got nil")
	}
}

func TestData_prefixedPathReturnsError(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	_, err := c.Data(t.Context(), "data.roles")
	if err == nil {
		t.Fatal("'data.' prefix: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "data.") {
		t.Fatalf("error should mention 'data.' prefix: %v", err)
	}
}

func TestData_cachesPreparedQuery(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	ctx := t.Context()
	if _, err := c.Data(ctx, "roles"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, ok := c.dataCache.Load("roles"); !ok {
		t.Fatal("cache miss after first call")
	}
	if _, err := c.Data(ctx, "roles"); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestData_concurrentSinglePrepare(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	ctx := t.Context()

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for range 50 {
		wg.Go(func() {
			if _, err := c.Data(ctx, "permissions"); err != nil {
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
	c.dataCache.Range(func(k, _ any) bool {
		if k == "permissions" {
			n++
		}
		return true
	})
	if n != 1 {
		t.Fatalf("cache entries for 'permissions' = %d, want 1", n)
	}
}

// Sanity: compile-time guard that the test file references authz.Input
// somewhere so that import isn't orphaned if a test is removed later.
var _ = authz.Input{}
