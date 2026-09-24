package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/m2sound2456/staffdisplay/backend/internal/audit"
	"github.com/m2sound2456/staffdisplay/backend/internal/auth"
	"github.com/m2sound2456/staffdisplay/backend/internal/config"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/internal/logger"
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
	cfg         *config.Config
	db          *database.Database
	authService *auth.Service
	startedAt   time.Time
}

func newHandlers(cfg *config.Config, db *database.Database) *handlers {
	return newHandlersWithAuth(cfg, db, newAuthService(cfg, db))
}

// newHandlersWithAuth builds the handler set with an explicit authentication
// service, so tests can drive the HTTP surface without PostgreSQL.
func newHandlersWithAuth(cfg *config.Config, db *database.Database, authService *auth.Service) *handlers {
	return &handlers{
		cfg:         cfg,
		db:          db,
		authService: authService,
		startedAt:   time.Now().UTC(),
	}
}

// newAuthService wires the FG4 authentication service from the configuration and
// the database handle. A missing signing key (a hand built configuration, never
// a validated deployment) leaves the service unconfigured: authentication then
// answers 500 instead of accepting everything.
func newAuthService(cfg *config.Config, db *database.Database) *auth.Service {
	var gormDB *gorm.DB
	if db != nil {
		gormDB = db.Gorm()
	}

	var signer *auth.Signer
	if cfg != nil {
		built, err := auth.NewSigner(auth.SignerOptions{
			Secret:          cfg.Auth.JWTSecret,
			PreviousSecrets: cfg.Auth.PreviousSecrets,
			AccessTokenTTL:  cfg.Auth.AccessTokenTTL,
		})
		switch {
		case err != nil:
			logger.Error("auth_signer_unavailable", zap.Error(err))
		default:
			signer = built
			if window := built.RotationWindow(); window > 0 {
				logger.Info("auth_rotation_window_active",
					zap.Int("previous_keys", window),
					zap.String("policy", "verify with the rotation window, sign with the current key"),
				)
			}
		}
	}

	var refreshTTL time.Duration
	if cfg != nil {
		refreshTTL = cfg.Auth.RefreshTokenTTL
	}

	return auth.NewService(auth.ServiceOptions{
		Repository:      auth.NewRepository(gormDB),
		Audit:           audit.NewRepository(gormDB),
		Signer:          signer,
		RefreshTokenTTL: refreshTTL,
	})
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
