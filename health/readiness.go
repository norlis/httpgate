package health

import (
	"net/http"
	"sync/atomic"

	"github.com/norlis/httpgate/presenter"
)

// Readiness gates an inner readiness handler with a drain switch. After
// MarkDraining (e.g. wired to server.OnShutdown on SIGTERM) it reports 503
// regardless of inner, so the load balancer stops routing before shutdown.
type Readiness struct {
	inner    http.Handler
	draining atomic.Bool
}

// NewReadiness wraps inner (may be a *Probe; nil means "always ready").
func NewReadiness(inner http.Handler) *Readiness {
	return &Readiness{inner: inner}
}

// MarkDraining flips the gate to not-ready; subsequent requests get 503.
func (r *Readiness) MarkDraining() { r.draining.Store(true) }

func (r *Readiness) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.draining.Load() {
		presenter.PlainText(w, req, "draining", presenter.WithStatusCode(http.StatusServiceUnavailable))
		return
	}
	if r.inner == nil {
		presenter.PlainText(w, req, "OK")
		return
	}
	r.inner.ServeHTTP(w, req)
}
