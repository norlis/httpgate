package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/norlis/httpgate/authz"
	"github.com/norlis/httpgate/logging"
	"github.com/norlis/httpgate/trace"
)

// fakeEnforcer is the test double for authz.Enforcer; it records the last
// Input so tests can assert the action format.
type fakeEnforcer struct {
	allow bool
	err   error
	last  authz.Input
}

func (f *fakeEnforcer) IsAllowed(_ context.Context, in authz.Input) (bool, error) {
	f.last = in
	return f.allow, f.err
}

func rolesExtractor(r *http.Request) (map[string]any, error) {
	if r.Header.Get("X-Roles") == "" {
		return nil, errors.New("missing X-Roles header")
	}
	return map[string]any{"roles": strings.Split(r.Header.Get("X-Roles"), ",")}, nil
}

func serveAuthorize(t *testing.T, mw Middleware, target, roles string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, target, http.NoBody)
	if roles != "" {
		req.Header.Set("X-Roles", roles)
	}
	req = req.WithContext(trace.NewContext(req.Context(), trace.Context{TraceID: "trace-123", SpanID: "0123456789abcdef"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, called
}

func problemBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var pd map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pd); err != nil {
		t.Fatalf("body: %v (%q)", err, rec.Body.String())
	}
	return pd
}

func TestAuthorize_AllowedReachesHandlerWithActionAndPayload(t *testing.T) {
	t.Parallel()
	e := &fakeEnforcer{allow: true}
	rec, called := serveAuthorize(t, Authorize(e, rolesExtractor), "/api/items?page=2", "admin,ops")
	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("called=%v status=%d", called, rec.Code)
	}
	if e.last.Action != "GET:/api/items?page=2" {
		t.Fatalf("action = %q", e.last.Action)
	}
	if roles, _ := e.last.Payload["roles"].([]string); len(roles) != 2 || roles[0] != "admin" {
		t.Fatalf("payload = %v", e.last.Payload)
	}
}

func TestAuthorize_DeniedIs403Problem(t *testing.T) {
	t.Parallel()
	rec, called := serveAuthorize(t, Authorize(&fakeEnforcer{allow: false}, rolesExtractor), "/api/x", "guest")
	if called || rec.Code != http.StatusForbidden {
		t.Fatalf("called=%v status=%d", called, rec.Code)
	}
	pd := problemBody(t, rec)
	if pd["title"] != "access denied" || pd["detail"] != "You do not have permission to perform this action." ||
		pd["instance"] != "/api/x" || pd["requestId"] != "trace-123" {
		t.Fatalf("problem = %v", pd)
	}
}

func TestAuthorize_ExtractorErrorIs400WithMessage(t *testing.T) {
	t.Parallel()
	rec, called := serveAuthorize(t, Authorize(&fakeEnforcer{allow: true}, rolesExtractor), "/api/x", "")
	if called || rec.Code != http.StatusBadRequest {
		t.Fatalf("called=%v status=%d", called, rec.Code)
	}
	pd := problemBody(t, rec)
	if pd["title"] != "Bad Request" || pd["detail"] != "missing X-Roles header" || pd["requestId"] != "trace-123" {
		t.Fatalf("problem = %v", pd)
	}
}

// B2: an enforcer failure is a server fault. The raw error must be logged, not
// sent to the client.
func TestAuthorize_EnforcerErrorIs500LoggedNotLeaked(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	e := &fakeEnforcer{err: errors.New("rego: undefined ref data.authz.allow at policies/authz.rego:12")}
	rec, called := serveAuthorize(t, Authorize(e, rolesExtractor, WithAuthorizeLogger(log)), "/api/x", "admin")
	if called || rec.Code != http.StatusInternalServerError {
		t.Fatalf("called=%v status=%d", called, rec.Code)
	}
	pd := problemBody(t, rec)
	if strings.Contains(rec.Body.String(), "rego") || pd["detail"] != nil {
		t.Fatalf("enforcer error leaked to the client: %s", rec.Body.String())
	}
	if pd["title"] != "Internal Server Error" || pd["requestId"] != "trace-123" {
		t.Fatalf("problem = %v", pd)
	}
	line := lastLine(t, buf)
	if line[logging.KeyMessage] != "server error" || !strings.Contains(line[logging.KeyErrorMessage].(string), "rego: undefined ref") ||
		line[logging.KeyHTTPResponseStatusCode] != float64(500) {
		t.Fatalf("log line = %v", line)
	}
}

func TestAuthorize_EnforcerErrorWithoutLoggerStillHidesError(t *testing.T) {
	t.Parallel()
	e := &fakeEnforcer{err: errors.New("secret internals")}
	rec, _ := serveAuthorize(t, Authorize(e, rolesExtractor), "/api/x", "admin")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
