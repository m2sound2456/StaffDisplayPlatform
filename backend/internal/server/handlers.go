package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/internal/version"
)

// healthCheckProbeTimeout bounds the dependency probe performed by health/ready.
const healthCheckProbeTimeout = 2 * time.Second

// Health status values.
const (
	healthStatusOK          = "ok"
	healthStatusDegraded    = "degraded"
	healthStatusUnavailable = "unavailable"
)

type healthCheck struct {
	Status    string   `json:"status"`
	LatencyMS *float64 `json:"latency_ms,omitempty"`
	Message   string   `json:"message,omitempty"`
}

type healthResponse struct {
	Status        string                 `json:"status"`
	Service       string                 `json:"service"`
	Environment   string                 `json:"environment"`
	Version       string                 `json:"version"`
	UptimeSeconds float64                `json:"uptime_seconds"`
	ServerTime    string                 `json:"server_time"`
	Checks        map[string]healthCheck `json:"checks"`
}

type versionResponse struct {
	Version     string `json:"version"`
	GitCommit   string `json:"git_commit"`
	BuildTime   string `json:"build_time"`
	GoVersion   string `json:"go_version"`
	Environment string `json:"environment"`
}

// handlers holds the dependencies of every HTTP handler.
type handlers struct {
	cfg       *config.Config
	db        *database.Database
	startedAt time.Time
}

func newHandlers(cfg *config.Config, db *database.Database) *handlers {
	return &handlers{cfg: cfg, db: db, startedAt: time.Now().UTC()}
}

// liveness answers "is the process alive" and never touches dependencies.
func (h *handlers) liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": h.serviceName(),
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

// readiness answers "can this instance serve traffic" (used by nginx/LB).
func (h *handlers) readiness(c *gin.Context) {
	report := h.buildHealth(c.Request.Context())
	status := http.StatusOK
	if report.Status != healthStatusOK {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, report)
}

// health returns the detailed dependency report under the API envelope.
func (h *handlers) health(c *gin.Context) {
	OK(c, h.buildHealth(c.Request.Context()))
}

// version returns build metadata for operators and support.
func (h *handlers) version(c *gin.Context) {
	OK(c, versionResponse{
		Version:     version.Version,
		GitCommit:   version.GitCommit,
		BuildTime:   version.BuildTime,
		GoVersion:   version.GoVersion(),
		Environment: h.cfg.App.Environment,
	})
}

func (h *handlers) serviceName() string {
	if h.cfg != nil && h.cfg.App.Name != "" {
		return h.cfg.App.Name + "-api"
	}
	return "staffdisplay-api"
}

func (h *handlers) buildHealth(ctx context.Context) healthResponse {
	checks := make(map[string]healthCheck, 1)
	overall := healthStatusOK

	probeCtx, cancel := context.WithTimeout(ctx, healthCheckProbeTimeout)
	defer cancel()

	switch {
	case h.db == nil:
		checks["database"] = healthCheck{
			Status:  healthStatusUnavailable,
			Message: "database is not configured",
		}
		overall = healthStatusUnavailable
	default:
		if latency, err := h.db.PingLatency(probeCtx); err != nil {
			// Never leak the DSN or driver error to an unauthenticated caller.
			checks["database"] = healthCheck{
				Status:  healthStatusUnavailable,
				Message: "database unreachable",
			}
			overall = healthStatusUnavailable
		} else {
			latencyMS := float64(latency.Microseconds()) / 1000.0
			checks["database"] = healthCheck{Status: healthStatusOK, LatencyMS: &latencyMS}
		}
	}

	return healthResponse{
		Status:        overall,
		Service:       h.serviceName(),
		Environment:   h.cfg.App.Environment,
		Version:       h.cfg.App.Version,
		UptimeSeconds: time.Since(h.startedAt).Seconds(),
		ServerTime:    time.Now().UTC().Format(time.RFC3339),
		Checks:        checks,
	}
}
