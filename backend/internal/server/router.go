// Package server assembles the HTTP surface of the Staff Display Platform.
//
// Everything lives on a single domain with path based routing (BLUEPRINT §32):
//
//	/healthz /readyz   probes
//	/api/v1/...        REST API (store scoped, tenant isolated)
//	/ws                realtime (FG22)
//
// The SPA (admin, display, device setup) is served as static files by nginx and
// only talks to /api on the same origin.
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
)

// NewRouter builds the gin engine with the platform middleware stack and every
// route group that exists today. Feature Group handlers are registered here as
// they land (see docs/BLUEPRINT.md §25).
func NewRouter(cfg *config.Config, db *database.Database) *gin.Engine {
	if cfg == nil {
		fallback := config.Default()
		cfg = &fallback
	}

	if config.IsProduction(cfg) {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false

	// Local development may run without an explicit allow-list; production must
	// configure cors.allowed_origins (enforced by config validation).
	allowUnknownOrigins := !config.IsProduction(cfg) && len(cfg.CORS.AllowedOrigins) == 0

	router.Use(
		RequestID(),
		AccessLog(),
		Recovery(),
		SecurityHeaders(),
		CORS(cfg.CORS.AllowedOrigins, allowUnknownOrigins),
	)

	api := newHandlers(cfg, db)

	router.NoRoute(func(c *gin.Context) { NotFound(c, "endpoint not found") })
	router.NoMethod(func(c *gin.Context) {
		Fail(c, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed", nil)
	})

	// --- probes (unauthenticated, not versioned) ---------------------------
	router.GET("/healthz", api.liveness)
	router.GET("/readyz", api.readiness)

	// --- API v1 -----------------------------------------------------------
	v1 := router.Group("/api/v1")
	{
		v1.GET("/health", api.health)
		v1.GET("/version", api.version)

		// FG4  — authentication:      POST /auth/login, /auth/logout, GET /auth/me
		// FG5  — stores:              GET|POST /stores, GET|PUT|DELETE /stores/:id
		// FG6  — employees:           GET|POST /stores/:storeId/employees, ...
		// FG16 — devices + pairing:   POST /stores/:storeId/devices/pairing, ...
		// FG10 — display bootstrap:   GET /display/bootstrap
	}

	// FG22 — realtime: router.GET("/ws", realtime.Handler(...))
	return router
}
