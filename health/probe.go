// Package health provides HTTP health probes (liveness and readiness).
package health

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/norlis/httpgate/logging"
	"github.com/norlis/httpgate/presenter"
)

// Checker is implemented by anything the Probe can interrogate. It is
// defined here because Probe is the only consumer; producers (DB clients,
// remote APIs, etc.) satisfy it structurally.
type Checker interface {
	Check(ctx context.Context) error
}

// CheckResult holds the detailed result of a single health check.
type CheckResult struct {
	Status   string `json:"status"`          // "OK", "FAIL" or "TIMEOUT" (checker ignored its deadline).
	Duration string `json:"duration"`        // Check duration, e.g. "1.5ms".
	Error    string `json:"error,omitempty"` // Error message when status is "FAIL"; only with WithErrorDetails.
}

// Probe is an HTTP handler that runs a set of health checks.
type Probe struct {
	checkers     map[string]Checker
	checkTimeout time.Duration // 0 ⇒ no per-check deadline beyond the request's
	errorDetails bool          // expose checker errors in the body
	logger       *slog.Logger  // nil ⇒ failures are not logged
}

// ProbeOption configures NewProbe.
type ProbeOption func(*Probe)

// WithCheckTimeout bounds each checker with its own context deadline, so one
// hung dependency fails fast instead of holding the probe until the kubelet's
// own timeout. It also bounds the probe itself: a checker that ignores ctx is
// reported as TIMEOUT once the deadline passes instead of hanging the request.
// Keep it below the probe's timeoutSeconds.
func WithCheckTimeout(d time.Duration) ProbeOption {
	return func(p *Probe) { p.checkTimeout = d }
}

// WithErrorDetails puts each failing checker's error message in the response
// body. Off by default: the message usually names internal hosts and
// credentials-adjacent details, which belong in the log (WithLogger), not in a
// probe that a load balancer or anyone on the network may read.
func WithErrorDetails() ProbeOption {
	return func(p *Probe) { p.errorDetails = true }
}

// WithLogger logs every failing or timed-out check as "health check failed"
// with the standard error object and the check name. A nil logger is ignored.
func WithLogger(l *slog.Logger) ProbeOption {
	return func(p *Probe) {
		if l != nil {
			p.logger = l
		}
	}
}

// NewProbe builds a Probe over the given named checkers. A nil map yields a
// probe that always reports healthy.
func NewProbe(checkers map[string]Checker, opts ...ProbeOption) *Probe {
	if checkers == nil {
		checkers = make(map[string]Checker)
	}
	p := &Probe{checkers: checkers}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type checkOutcome struct {
	name     string
	err      error
	duration time.Duration
}

// ServeHTTP implements the http.Handler interface. Checks run concurrently;
// the wait is bounded by the request context and, when set, by checkTimeout.
// A checker still running past that bound is reported as TIMEOUT and its
// goroutine is left to finish on its own (it has nowhere to write to).
func (p *Probe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(p.checkers) == 0 {
		presenter.PlainText(w, r, "OK")
		return
	}

	ctx := r.Context()
	var cancel context.CancelFunc
	if p.checkTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, p.checkTimeout)
		defer cancel()
	}

	outcomes := make(chan checkOutcome, len(p.checkers))
	for name, checker := range p.checkers {
		go func() {
			start := time.Now()
			err := checker.Check(ctx)
			outcomes <- checkOutcome{name: name, err: err, duration: time.Since(start)}
		}()
	}

	results := make(map[string]CheckResult, len(p.checkers))
	httpStatus := http.StatusOK
	done := ctx.Done()         // nil after the first fire so the case is not re-entered
	var grace <-chan time.Time // armed once ctx is done
	for len(results) < len(p.checkers) {
		select {
		case o := <-outcomes:
			results[o.name] = p.record(ctx, o, &httpStatus)
		case <-done:
			// Checkers that honor ctx return right about now; give them a
			// moment before declaring the rest TIMEOUT.
			done, grace = nil, time.After(timeoutGrace)
		case <-grace:
			httpStatus = http.StatusServiceUnavailable
			p.markTimedOut(ctx, results)
		}
	}

	presenter.JSON(w, r, results, presenter.WithStatusCode(httpStatus))
}

// markTimedOut records TIMEOUT for every checker without a result yet, logging
// each one like a failure. After this, results covers all checkers and the
// collection loop ends.
func (p *Probe) markTimedOut(ctx context.Context, results map[string]CheckResult) {
	for name := range p.checkers {
		if _, done := results[name]; done {
			continue
		}
		results[name] = CheckResult{Status: "TIMEOUT", Duration: p.checkTimeout.String()}
		if p.logger != nil {
			p.logger.LogAttrs(ctx, slog.LevelError, "health check failed",
				slog.String("health.check", name), slog.String("health.status", "TIMEOUT"))
		}
	}
}

// timeoutGrace is how long ServeHTTP keeps collecting results after the
// deadline, so checkers that honor ctx are reported as FAIL with their own
// error rather than racing the deadline into TIMEOUT.
const timeoutGrace = 20 * time.Millisecond

// record converts one outcome into its CheckResult, logging failures and
// flipping the overall status to 503.
func (p *Probe) record(ctx context.Context, o checkOutcome, httpStatus *int) CheckResult {
	res := CheckResult{Status: "OK", Duration: o.duration.String()}
	if o.err == nil {
		return res
	}
	*httpStatus = http.StatusServiceUnavailable
	res.Status = "FAIL"
	if p.errorDetails {
		res.Error = o.err.Error()
	}
	if p.logger != nil {
		p.logger.LogAttrs(ctx, slog.LevelError, "health check failed",
			slog.String("health.check", o.name), logging.Err(o.err))
	}
	return res
}
