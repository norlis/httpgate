// Package health provides HTTP health probes (liveness and readiness).
package health

import (
	"context"
	"net/http"
	"sync"
	"time"

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
	Status   string `json:"status"`          // "OK" or "FAIL"
	Duration string `json:"duration"`        // Check duration, e.g. "1.5ms".
	Error    string `json:"error,omitempty"` // Error message when status is "FAIL".
}

// Probe is an HTTP handler that runs a set of health checks.
type Probe struct {
	checkers map[string]Checker
}

// NewProbe builds a Probe over the given named checkers. A nil map yields a
// probe that always reports healthy.
func NewProbe(checkers map[string]Checker) *Probe {
	if checkers == nil {
		checkers = make(map[string]Checker)
	}
	return &Probe{
		checkers: checkers,
	}
}

// ServeHTTP implements the http.Handler interface.
func (p *Probe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(p.checkers) == 0 {
		presenter.PlainText(w, r, "OK")
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	results := make(map[string]CheckResult, len(p.checkers))
	httpStatus := http.StatusOK

	for name, checker := range p.checkers {
		wg.Go(func() {
			start := time.Now()
			err := checker.Check(r.Context())
			duration := time.Since(start)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				httpStatus = http.StatusServiceUnavailable
				results[name] = CheckResult{
					Status:   "FAIL",
					Duration: duration.String(),
					Error:    err.Error(),
				}
			} else {
				results[name] = CheckResult{
					Status:   "OK",
					Duration: duration.String(),
				}
			}
		})
	}

	wg.Wait()

	presenter.JSON(w, r, results, presenter.WithStatusCode(httpStatus))
}
