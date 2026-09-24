package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/m2sound2456/staffdisplay/backend/internal/logger"
)

// TestMain keeps the test output readable: request logging runs at info level in
// production, so only errors would be emitted here.
func TestMain(m *testing.M) {
	_ = logger.Init(logger.Options{Development: false, Level: "error", Encoding: "console"})
	os.Exit(m.Run())
}

func TestRecoveryConvertsPanicIntoErrorEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Recovery())
	engine.GET("/boom", func(c *gin.Context) {
		panic("database exploded")
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}

	var envelope struct {
		Error ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("response is not a JSON error envelope: %s", recorder.Body.String())
	}
	if envelope.Error.Code != CodeInternalError {
		t.Errorf("error code = %q, want %q", envelope.Error.Code, CodeInternalError)
	}
	if envelope.Error.Message != "internal server error" {
		t.Errorf("error message = %q, want the generic message", envelope.Error.Message)
	}
}

func TestRecoveryKeepsSuccessfulRequestsIntact(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Recovery())
	engine.GET("/ok", func(c *gin.Context) { OK(c, gin.H{"hello": "world"}) })

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestRequestIDMiddlewareGeneratesUniqueIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RequestID())
	engine.GET("/id", func(c *gin.Context) { c.String(http.StatusOK, RequestIDOf(c)) })

	first := httptest.NewRecorder()
	engine.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/id", nil))

	second := httptest.NewRecorder()
	engine.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/id", nil))

	if first.Body.String() == "" || first.Body.String() == second.Body.String() {
		t.Fatalf("request ids must be generated uniquely, got %q and %q", first.Body.String(), second.Body.String())
	}
	if first.Header().Get(RequestIDHeader) != first.Body.String() {
		t.Error("the context request id must match the response header")
	}
}

func TestRequestIDMiddlewareRejectsOversizedClientValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RequestID())
	engine.GET("/id", func(c *gin.Context) { c.String(http.StatusOK, RequestIDOf(c)) })

	request := httptest.NewRequest(http.MethodGet, "/id", nil)
	request.Header.Set(RequestIDHeader, strings.Repeat("a", 500))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if len(recorder.Body.String()) != 36 {
		t.Fatalf("oversized request id must be replaced by a generated UUID, got %q", recorder.Body.String())
	}
}
