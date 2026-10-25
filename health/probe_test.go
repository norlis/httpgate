package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/norlis/httpgate/logging"
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

type hangingChecker struct{}

func (hangingChecker) Check(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestProbe_checkTimeoutBoundsHangingChecker(t *testing.T) {
	t.Parallel()
	p := NewProbe(map[string]Checker{"db": hangingChecker{}}, WithCheckTimeout(30*time.Millisecond), WithErrorDetails())
	rr := httptest.NewRecorder()
	start := time.Now()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	if took := time.Since(start); took > time.Second {
		t.Fatalf("probe took %v; the per-check timeout did not apply", took)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	var results map[string]CheckResult
	if err := json.NewDecoder(rr.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if results["db"].Status != "FAIL" || !strings.Contains(results["db"].Error, "deadline exceeded") {
		t.Fatalf("db result = %+v", results["db"])
	}
}

func TestProbe_defaultHasNoExtraDeadline(t *testing.T) {
	t.Parallel()
	ch := &ctxChecker{}
	p := NewProbe(map[string]Checker{"db": ch})
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/live", http.NoBody))
	if _, ok := ch.seenCtx.Deadline(); ok {
		t.Fatal("without WithCheckTimeout the checker context must carry no deadline")
	}
}

// ignoringChecker never looks at ctx: the dependency client that hangs.
type ignoringChecker struct{ release chan struct{} }

func (c ignoringChecker) Check(context.Context) error { <-c.release; return nil }

// B7: a checker that ignores ctx must not hang the probe past the per-check
// timeout; it is reported as TIMEOUT and the others keep their result.
func TestProbe_checkerIgnoringContextIsBoundedByCheckTimeout(t *testing.T) {
	t.Parallel()
	hung := ignoringChecker{release: make(chan struct{})}
	defer close(hung.release)
	p := NewProbe(map[string]Checker{"hung": hung, "ok": &ctxChecker{}}, WithCheckTimeout(30*time.Millisecond))
	rr := httptest.NewRecorder()
	start := time.Now()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	if took := time.Since(start); took > time.Second {
		t.Fatalf("probe took %v; a checker ignoring ctx must not hang it", took)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	var results map[string]CheckResult
	if err := json.NewDecoder(rr.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if results["hung"].Status != "TIMEOUT" || results["ok"].Status != "OK" {
		t.Fatalf("results = %+v", results)
	}
}

// B7: the request context (client gone, server timeout) also bounds the wait.
func TestProbe_requestCancellationBoundsTheWait(t *testing.T) {
	t.Parallel()
	hung := ignoringChecker{release: make(chan struct{})}
	defer close(hung.release)
	p := NewProbe(map[string]Checker{"hung": hung})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	rr := httptest.NewRecorder()
	start := time.Now()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody).WithContext(ctx))
	if took := time.Since(start); took > time.Second {
		t.Fatalf("probe took %v", took)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

// B8: checker errors are not exposed in the body by default; they go to the
// logger. WithErrorDetails restores the error field.
func TestProbe_hidesCheckerErrorsByDefaultAndLogsThem(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	log := logging.New(buf)
	p := NewProbe(map[string]Checker{"db": &ctxChecker{err: errors.New("dial tcp 10.0.0.5:5432: refused")}}, WithLogger(log))
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	if rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "10.0.0.5") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var results map[string]CheckResult
	_ = json.NewDecoder(rr.Body).Decode(&results)
	if results["db"].Status != "FAIL" || results["db"].Error != "" {
		t.Fatalf("result = %+v", results["db"])
	}
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("log: %v (%s)", err, buf.String())
	}
	if line[logging.KeyMessage] != "health check failed" || line["health.check"] != "db" ||
		!strings.Contains(line[logging.KeyErrorMessage].(string), "10.0.0.5") {
		t.Fatalf("log line = %v", line)
	}
}

func TestProbe_withErrorDetailsExposesError(t *testing.T) {
	t.Parallel()
	p := NewProbe(map[string]Checker{"db": &ctxChecker{err: errors.New("offline")}}, WithErrorDetails())
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	var results map[string]CheckResult
	_ = json.NewDecoder(rr.Body).Decode(&results)
	if results["db"].Error != "offline" {
		t.Fatalf("result = %+v", results["db"])
	}
}

// Characterization before extracting the TIMEOUT marking: a checker still
// running past the deadline is reported TIMEOUT, 503 overall, and logged with
// the same message and check name as a FAIL.
func TestProbe_timedOutCheckerIsLoggedAsTimeout(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	release := make(chan struct{})
	defer close(release)
	p := NewProbe(map[string]Checker{"slow": ignoringChecker{release: release}}, WithCheckTimeout(20*time.Millisecond), WithLogger(logging.New(buf)))
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", http.NoBody))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	var results map[string]CheckResult
	if err := json.NewDecoder(rr.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if results["slow"].Status != "TIMEOUT" || results["slow"].Duration != "20ms" {
		t.Fatalf("result = %+v", results["slow"])
	}
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("log: %v (%s)", err, buf.String())
	}
	if line[logging.KeyMessage] != "health check failed" || line["health.check"] != "slow" || line["health.status"] != "TIMEOUT" {
		t.Fatalf("log line = %v", line)
	}
}
