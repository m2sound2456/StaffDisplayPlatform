package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
)

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodGet, "/healthz", map[string]string{"Origin": "http://localhost:5173"})
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the configured origin", got)
	}
	if !strings.Contains(recorder.Header().Get("Vary"), "Origin") {
		t.Error("response must vary on Origin")
	}
}

func TestCORSRejectsUnlistedOrigin(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodGet, "/healthz", map[string]string{"Origin": "https://evil.example.com"})
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for an unlisted origin", got)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the browser enforces the block)", recorder.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodOptions, "/api/v1/health", map[string]string{
		"Origin":                        "http://localhost:5173",
		"Access-Control-Request-Method": http.MethodGet,
	})
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", recorder.Code, recorder.Body.String())
	}
	if methods := recorder.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(methods, http.MethodGet) {
		t.Errorf("Access-Control-Allow-Methods = %q, want it to include GET", methods)
	}
	if headers := recorder.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(headers, "X-Device-Token") {
		t.Errorf("Access-Control-Allow-Headers = %q, want it to include X-Device-Token", headers)
	}
}

func TestCORSDevelopmentWithoutAllowListReflectsOrigin(t *testing.T) {
	cfg := testConfig()
	cfg.CORS.AllowedOrigins = nil
	router := NewRouter(cfg, nil)

	recorder := perform(router, http.MethodGet, "/healthz", map[string]string{"Origin": "http://192.168.1.50:5173"})
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://192.168.1.50:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the requesting origin in development", got)
	}
}

func TestCORSProductionPreflightFromUnknownOriginIsForbidden(t *testing.T) {
	cfg := config.Default()
	cfg.App.Environment = config.EnvProduction
	cfg.CORS.AllowedOrigins = []string{"https://display.example.com"}
	router := NewRouter(&cfg, nil)

	recorder := perform(router, http.MethodOptions, "/api/v1/health", map[string]string{
		"Origin":                        "https://attacker.example.com",
		"Access-Control-Request-Method": http.MethodGet,
	})
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a production preflight from an unlisted origin", recorder.Code)
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodGet, "/healthz", nil)
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if recorder.Header().Get("Referrer-Policy") == "" {
		t.Error("Referrer-Policy must be set")
	}
	if recorder.Header().Get("X-Frame-Options") == "" {
		t.Error("X-Frame-Options must be set")
	}
}

func TestRouterUsesDefaultConfigWhenNil(t *testing.T) {
	router := NewRouter(nil, nil)

	recorder := perform(router, http.MethodGet, "/healthz", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even with a nil config", recorder.Code)
	}
}
