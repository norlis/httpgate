package health

import (
	"net/http"
	"os"
	"time"

	"github.com/norlis/httpgate/presenter"
)

// Status is an HTTP handler that reports process uptime, hostname and version.
type Status struct {
	StartedAt time.Time `json:"startedAt"`
	Hostname  string    `json:"hostname"`
	Version   string    `json:"version"`
}

// NewStatus builds a Status stamped with the current time as StartedAt. The
// hostname falls back to "localhost" if it cannot be determined.
func NewStatus(version string) *Status {
	hostname, err := os.Hostname()
	if hostname == "" || err != nil {
		hostname = "localhost"
	}

	return &Status{
		StartedAt: time.Now().UTC(),
		Hostname:  hostname,
		Version:   version,
	}
}

// uptime returns how long the process has been running.
func (u *Status) uptime() time.Duration {
	return time.Since(u.StartedAt)
}

func (u *Status) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	presenter.JSON(w, r, struct {
		Uptime   string `json:"uptime"`
		Hostname string `json:"hostname"`
		Version  string `json:"version"`
	}{
		Uptime:   u.uptime().String(),
		Hostname: u.Hostname,
		Version:  u.Version,
	})
}
