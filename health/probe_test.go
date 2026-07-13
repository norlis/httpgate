package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type ctxChecker struct {
	seenCtx context.Context
	err     error
}

func (c *ctxChecker) Check(ctx context.Context) error {
	c.seenCtx = ctx
	return c.err
}

func TestProbe_propagatesRequestContext(t *testing.T) {
	t.Parallel()
	ch := &ctxChecker{}
	p := NewProbe(map[string]Checker{"db": ch})

	req := httptest.NewRequest("GET", "/live", http.NoBody)
	type k struct{}
	ctx := context.WithValue(req.Context(), k{}, "marker")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if ch.seenCtx == nil {
		t.Fatal("checker did not receive context")
	}
	if ch.seenCtx.Value(k{}) != "marker" {
		t.Fatal("checker received a different context than the request's")
	}
}

func TestProbe_returns503OnFailure(t *testing.T) {
	t.Parallel()
	p := NewProbe(map[string]Checker{
		"ok":   &ctxChecker{},
		"fail": &ctxChecker{err: errors.New("offline")},
	})
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestProbe_emptyReturns200(t *testing.T) {
	t.Parallel()
	p := NewProbe(nil)
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/live", http.NoBody))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestCheckResult_DurationIsFormattedString(t *testing.T) {
	t.Parallel()
	p := NewProbe(map[string]Checker{"db": &ctxChecker{}})
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/live", http.NoBody))

	var body map[string]map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	dur, ok := body["db"]["duration"]
	if !ok {
		t.Fatal("missing duration field")
	}
	s, isStr := dur.(string)
	if !isStr {
		t.Fatalf("duration type = %T, want string", dur)
	}
	// time.Duration.String() always carries a unit; the value must round-trip
	// back into a valid duration.
	if _, err := time.ParseDuration(s); err != nil {
		t.Fatalf("duration %q is not a valid time.Duration string: %v", s, err)
	}
}
