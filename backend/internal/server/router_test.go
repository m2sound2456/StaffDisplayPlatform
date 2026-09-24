package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/m2sound2456/staffdisplay/backend/internal/config"
)

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.App.Environment = config.EnvDevelopment
	cfg.CORS.AllowedOrigins = []string{"http://localhost:5173"}
	return &cfg
}

func perform(handler http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("response is not JSON (%d): %s", recorder.Code, recorder.Body.String())
	}
	if envelope.Data == nil {
		t.Fatalf("response has no data envelope: %s", recorder.Body.String())
	}
	return envelope.Data
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) ErrorBody {
	t.Helper()
	var envelope struct {
		Error ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("response is not a JSON error envelope (%d): %s", recorder.Code, recorder.Body.String())
	}
	if envelope.Error.Code == "" {
		t.Fatalf("error envelope has no code: %s", recorder.Body.String())
	}
	return envelope.Error
}

func TestLivenessProbe(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodGet, "/healthz", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal liveness body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["service"] != "staffdisplay-api" {
		t.Errorf("service = %v, want staffdisplay-api", body["service"])
	}
	if _, ok := body["time"]; !ok {
		t.Error("liveness body must contain a time field")
	}
}

func TestReadinessWithoutDatabaseIsUnavailable(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodGet, "/readyz", nil)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", recorder.Code, recorder.Body.String())
	}

	var report healthResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal readiness body: %v", err)
	}
	if report.Status != healthStatusUnavailable {
		t.Errorf("status = %q, want %q", report.Status, healthStatusUnavailable)
	}
	if report.Checks["database"].Status != healthStatusUnavailable {
		t.Errorf("database check = %q, want %q", report.Checks["database"].Status, healthStatusUnavailable)
	}
}

func TestAPIHealthReturnsEnvelope(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodGet, "/api/v1/health", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	data := decodeData(t, recorder)
	if data["service"] != "staffdisplay-api" {
		t.Errorf("service = %v, want staffdisplay-api", data["service"])
	}
	if data["environment"] != config.EnvDevelopment {
		t.Errorf("environment = %v, want %q", data["environment"], config.EnvDevelopment)
	}
	if _, ok := data["checks"]; !ok {
		t.Error("health payload must contain checks")
	}
}

func TestVersionEndpoint(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodGet, "/api/v1/version", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	data := decodeData(t, recorder)
	if version, _ := data["version"].(string); version == "" {
		t.Error("version must not be empty")
	}
	goVersion, _ := data["go_version"].(string)
	if !strings.HasPrefix(goVersion, "go") {
		t.Errorf("go_version = %q, want a go runtime version", goVersion)
	}
	if data["environment"] != config.EnvDevelopment {
		t.Errorf("environment = %v, want %q", data["environment"], config.EnvDevelopment)
	}
}

func TestUnknownRouteReturnsNotFoundEnvelope(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodGet, "/api/v1/does-not-exist", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if body := decodeError(t, recorder); body.Code != CodeNotFound {
		t.Errorf("error code = %q, want %q", body.Code, CodeNotFound)
	}
}

func TestWrongMethodReturnsMethodNotAllowed(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	recorder := perform(router, http.MethodPost, "/healthz", nil)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body %s)", recorder.Code, recorder.Body.String())
	}
	if body := decodeError(t, recorder); body.Code != CodeMethodNotAllowed {
		t.Errorf("error code = %q, want %q", body.Code, CodeMethodNotAllowed)
	}
}

func TestRequestIDIsGeneratedAndEchoed(t *testing.T) {
	router := NewRouter(testConfig(), nil)

	generated := perform(router, http.MethodGet, "/healthz", nil)
	if id := generated.Header().Get(RequestIDHeader); len(id) != 36 {
		t.Errorf("generated %s = %q, want a UUID", RequestIDHeader, id)
	}

	supplied := perform(router, http.MethodGet, "/healthz", map[string]string{RequestIDHeader: "abc-123"})
	if id := supplied.Header().Get(RequestIDHeader); id != "abc-123" {
		t.Errorf("echoed %s = %q, want abc-123", RequestIDHeader, id)
	}
}
