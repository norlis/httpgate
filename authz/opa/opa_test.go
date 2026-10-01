package opa

import (
	"errors"
	"fmt"
	"math"
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

func TestQuery_repeatedCallsStayConsistent(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)
	ctx := t.Context()
	first, err := c.Query(ctx, "data.roles", authz.Input{})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := c.Query(ctx, "data.roles", authz.Input{})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("cached query drifted: %v vs %v", first, second)
	}
	if n := len(c.state.Load().queries); n != 1 {
		t.Fatalf("prepared queries = %d, want 1", n)
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

func TestQuery_concurrentFirstUseIsSafe(t *testing.T) {
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
	if n := len(c.state.Load().queries); n != 1 {
		t.Fatalf("prepared queries = %d, want 1 (single prepare per query)", n)
	}
}

func memData(allowPath string) map[string]any {
	return map[string]any{
		"roles":       map[string]any{"admin": []any{"p1"}, "anonymous": []any{"public"}},
		"permissions": map[string]any{"p1": []any{allowPath}, "public": []any{"GET:/health$"}},
		"whitelist":   []any{"GET:/health$"},
	}
}

func newMemClient(t *testing.T, data map[string]any) *Client {
	t.Helper()
	c, err := New(t.Context(), Config{Query: "data.authz.allow", PoliciesPath: "testdata/policies"}, WithData(data))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func mustAllow(t *testing.T, c *Client, in authz.Input) bool {
	t.Helper()
	ok, err := c.IsAllowed(t.Context(), in)
	if err != nil {
		t.Fatalf("IsAllowed: %v", err)
	}
	return ok
}

func TestWithData_evaluatesAgainstMemoryDocument(t *testing.T) {
	t.Parallel()
	c := newMemClient(t, memData("GET:/mem$"))
	if !mustAllow(t, c, authz.Input{Payload: map[string]any{"roles": []string{"admin"}}, Action: "GET:/mem"}) {
		t.Fatal("admin should be allowed GET:/mem from the memory document")
	}
}

func TestWithData_ignoresJSONFilesUnderPoliciesPath(t *testing.T) {
	t.Parallel()
	c := newMemClient(t, memData("GET:/mem$"))
	in := authz.Input{Payload: map[string]any{"roles": []string{"admin"}}, Action: "GET:/stale"}
	if mustAllow(t, c, in) {
		t.Fatal("stale.json under PoliciesPath leaked into the data document")
	}
	if mustAllow(t, c, authz.Input{Action: "GET:/stale-public"}) {
		t.Fatal("stale.json whitelist leaked into the data document")
	}
}

func TestNew_rejectsDataFilesTogetherWithWithData(t *testing.T) {
	t.Parallel()
	_, err := New(t.Context(), Config{
		Query: "data.authz.allow", PoliciesPath: "testdata/policies", DataFiles: []string{"testdata/policies/stale.json"},
	}, WithData(memData("GET:/x$")))
	if !errors.Is(err, errBothDataSources) {
		t.Fatalf("err = %v, want errBothDataSources", err)
	}
}

func TestReload_changesDecision(t *testing.T) {
	t.Parallel()
	c := newMemClient(t, memData("GET:/v1$"))
	in := authz.Input{Payload: map[string]any{"roles": []string{"admin"}}, Action: "GET:/v2"}
	if mustAllow(t, c, in) {
		t.Fatal("GET:/v2 must be denied before reload")
	}
	if err := c.Reload(t.Context(), memData("GET:/v2$")); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !mustAllow(t, c, in) {
		t.Fatal("GET:/v2 must be allowed after reload")
	}
	if mustAllow(t, c, authz.Input{Payload: map[string]any{"roles": []string{"admin"}}, Action: "GET:/v1"}) {
		t.Fatal("GET:/v1 must be denied after reload (old document gone)")
	}
}

func TestReload_hintsFollowNewDocument(t *testing.T) {
	t.Parallel()
	c := newMemClient(t, memData("GET:/v1$"))
	if _, err := c.Permissions(t.Context(), adminInput()); err != nil {
		t.Fatalf("Permissions before: %v", err)
	}
	next := memData("GET:/v2$")
	next["roles"].(map[string]any)["admin"] = []any{"p1", "extra"}
	next["permissions"].(map[string]any)["extra"] = []any{"POST:/extra$"}
	if err := c.Reload(t.Context(), next); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	perms, err := c.Permissions(t.Context(), adminInput())
	if err != nil {
		t.Fatalf("Permissions after: %v", err)
	}
	if !sortedEqual(perms, []string{"p1", "extra"}) {
		t.Fatalf("permissions = %v, want [p1 extra]", perms)
	}
	res, err := c.AllowedResources(t.Context(), adminInput())
	if err != nil {
		t.Fatalf("AllowedResources after: %v", err)
	}
	if !sortedEqual(res, []string{"GET:/v2$", "POST:/extra$"}) {
		t.Fatalf("allowed_resources = %v", res)
	}
}

func TestReload_errorsOnDataFilesClient(t *testing.T) {
	t.Parallel()
	c := newTestClient(t) // file mode
	if err := c.Reload(t.Context(), memData("GET:/x$")); !errors.Is(err, errDataFilesMode) {
		t.Fatalf("err = %v, want errDataFilesMode", err)
	}
}

func TestReload_keepsPreviousStateOnFailure(t *testing.T) {
	t.Parallel()
	c := newMemClient(t, memData("GET:/v1$"))
	in := authz.Input{Payload: map[string]any{"roles": []string{"admin"}}, Action: "GET:/v1"}
	// OPA tolerates almost any data shape at prepare time, so the reliable
	// failure to exercise is the nil-document guard: it must return an error
	// without swapping state.
	before := c.state.Load()
	err := c.Reload(t.Context(), nil)
	if err == nil {
		t.Fatal("Reload(nil) must fail")
	}
	if c.state.Load() != before {
		t.Fatal("state pointer swapped despite the failure")
	}
	if !mustAllow(t, c, in) {
		t.Fatal("previous document must keep serving after a failed reload")
	}
}

func TestReload_concurrentFirstQueryIsKept(t *testing.T) {
	t.Parallel()
	c := newMemClient(t, memData("GET:/v1$"))
	ctx := t.Context()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := c.Reload(ctx, memData("GET:/v1$")); err != nil {
				t.Errorf("Reload: %v", err)
			}
		})
		wg.Go(func() {
			if _, err := c.Query(ctx, "data.authz.permissions", adminInput()); err != nil {
				t.Errorf("Query: %v", err)
			}
		})
		wg.Go(func() {
			if !mustAllow(t, c, authz.Input{Payload: map[string]any{"roles": []string{"admin"}}, Action: "GET:/v1"}) {
				t.Error("decision flipped during reload")
			}
		})
	}
	wg.Wait()
	if _, ok := c.state.Load().queries["data.authz.permissions"]; !ok {
		t.Fatal("query prepared during reloads was dropped")
	}
}

func TestNew_rejectsNilDataInsteadOfFallingBackToFiles(t *testing.T) {
	t.Parallel()
	var nilMap map[string]any
	_, err := New(t.Context(), Config{Query: "data.authz.allow", PoliciesPath: "testdata/policies"}, WithData(nilMap))
	if !errors.Is(err, errNilData) {
		t.Fatalf("err = %v, want errNilData (never silently read stale.json from disk)", err)
	}
}

func TestReload_unencodableDataReturnsErrorAndKeepsState(t *testing.T) {
	t.Parallel()
	c := newMemClient(t, memData("GET:/v1$"))
	before := c.state.Load()
	err := c.Reload(t.Context(), map[string]any{"roles": math.NaN()})
	if err == nil {
		t.Fatal("Reload with NaN must return an error, not panic")
	}
	if c.state.Load() != before {
		t.Fatal("state swapped despite the failure")
	}
	if !mustAllow(t, c, authz.Input{Payload: map[string]any{"roles": []string{"admin"}}, Action: "GET:/v1"}) {
		t.Fatal("previous document must keep serving")
	}
}

func TestNew_unencodableDataReturnsError(t *testing.T) {
	t.Parallel()
	_, err := New(t.Context(), Config{Query: "data.authz.allow", PoliciesPath: "testdata/policies"}, WithData(map[string]any{"x": make(chan int)}))
	if err == nil {
		t.Fatal("New with unencodable data must return an error, not panic")
	}
}
