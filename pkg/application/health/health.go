// Package health proporciona una implementación para sondas de salud HTTP (Liveness y Readiness).
package health

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/norlis/httpgate/pkg/port"
)

// CheckResult almacena el resultado detallado de una única comprobación de salud.
type CheckResult struct {
	Status   string `json:"status"`          // "OK" o "FAIL"
	Duration string `json:"duration"`        // Duración de la comprobación en formato legible.
	Error    string `json:"error,omitempty"` // Mensaje de error si el estado es "FAIL".
}

// Probe es un manejador HTTP que ejecuta un conjunto de comprobaciones de salud.
type Probe struct {
	checkers map[string]port.Checker
}

func NewProbe(checkers map[string]port.Checker) *Probe {
	if checkers == nil {
		checkers = make(map[string]port.Checker)
	}
	return &Probe{
		checkers: checkers,
	}
}

// ServeHTTP implementa la interfaz http.Handler.
func (p *Probe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(p.checkers) == 0 {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	results := make(map[string]CheckResult, len(p.checkers))
	httpStatus := http.StatusOK

	for name, checker := range p.checkers {
		wg.Add(1)
		go func(name string, checker port.Checker) {
			defer wg.Done()

			start := time.Now()
			err := checker.Check()
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
		}(name, checker)
	}

	wg.Wait()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(results)
}
