package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/auth"
)

// newProtectedEngine builds a gin engine that mirrors the production stack for a
// single test route: RequireAuth (+ RequireRole) around a handler that echoes the
// resolved principal. It follows the existing middleware_test.go convention of
// driving the production middleware with a test route.
func newProtectedEngine(t *testing.T, service *auth.Service, roles ...auth.Role) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	middleware := []gin.HandlerFunc{RequireAuth(service)}
	if len(roles) > 0 {
		middleware = append(middleware, RequireRole(roles...))
	}
	engine.GET("/protected", append(middleware, func(c *gin.Context) {
		principal := PrincipalOf(c)
		scope, ok := ScopeOf(c)
		OK(c, gin.H{
			"user_id":   principal.UserID().String(),
			"role":      principal.Role.String(),
			"tenant_id": principal.TenantID.String(),
			"scope_ok":  ok,
			"scope":     scope.TenantID.String(),
		})
	})...)
	return engine
}

// loginThroughService signs in and returns the session result.
func loginThroughService(t *testing.T, service *auth.Service, email string) *auth.SessionResult {
	t.Helper()

	result, err := service.Login(t.Context(), auth.LoginInput{
		Email:     email,
		Password:  testUserPassword,
		UserAgent: "Go-Test",
		IP:        "203.0.113.7",
	})
	if err != nil {
		t.Fatalf("Login(%s) error = %v", email, err)
	}
	return result
}

func TestRequireAuthResolvesThePrincipal(t *testing.T) {
	tenantID, storeID := uuid.New(), uuid.New()
	repository := &authTestRepository{}
	user := newTestAuthUser(t, tenantID, storeID, "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	service := newAuthTestService(t, repository, newServerTestClock())
	engine := newProtectedEngine(t, service)

	login := loginThroughService(t, service, user.Email)
	recorder := perform(engine, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer " + login.Tokens.AccessToken,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	data := decodeData(t, recorder)
	if data["user_id"] != user.ID.String() {
		t.Errorf("user_id = %v, want %s", data["user_id"], user.ID)
	}
	if data["tenant_id"] != tenantID.String() || data["scope"] != tenantID.String() {
		t.Errorf("tenant = %v / scope = %v, want %s", data["tenant_id"], data["scope"], tenantID)
	}
	if data["scope_ok"] != true {
		t.Error("ScopeOf() must resolve the tenant scope of a store admin")
	}
	if data["role"] != string(auth.RoleStoreAdmin) {
		t.Errorf("role = %v, want %q", data["role"], auth.RoleStoreAdmin)
	}
}

func TestRequireAuthRejectsARevokedOrDisabledAccount(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.New(), "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	service := newAuthTestService(t, repository, newServerTestClock())
	engine := newProtectedEngine(t, service)

	login := loginThroughService(t, service, user.Email)
	headers := map[string]string{AuthorizationHeader: "Bearer " + login.Tokens.AccessToken}

	if recorder := perform(engine, http.MethodGet, "/protected", headers); recorder.Code != http.StatusOK {
		t.Fatalf("status before revocation = %d, want 200", recorder.Code)
	}

	// Logout revokes the session behind the token: the next request is refused.
	principal, err := service.Authenticate(t.Context(), login.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if err := service.Logout(t.Context(), principal, "203.0.113.7"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if recorder := perform(engine, http.MethodGet, "/protected", headers); recorder.Code != http.StatusUnauthorized {
		t.Errorf("status after logout = %d, want 401", recorder.Code)
	}

	// A disabled account is refused as well, even with a token issued before the
	// change: the status is re-read from the account on every request.
	before := loginThroughService(t, service, user.Email)
	repository.mutate(user.ID, func(u *auth.User) { u.Status = auth.StatusDisabled })
	if recorder := perform(engine, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer " + before.Tokens.AccessToken,
	}); recorder.Code != http.StatusUnauthorized {
		t.Errorf("status for a disabled account = %d, want 401", recorder.Code)
	}
	if _, err := service.Login(t.Context(), auth.LoginInput{Email: user.Email, Password: testUserPassword}); err == nil {
		t.Error("a disabled account must not be able to sign in again")
	}
}

func TestRequireAuthAcceptsATokenOfTheRotationWindow(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.New(), "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	clock := newServerTestClock()
	service := newAuthTestService(t, repository, clock, testJWTPreviousSecret)
	engine := newProtectedEngine(t, service)

	// A credential issued before the rotation: same session, previous key.
	login := loginThroughService(t, service, user.Email)
	rotated := signTestToken(t, clock, testJWTPreviousSecret, login.Session.ID.String(), user)

	recorder := perform(engine, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer " + rotated,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a token of the rotation window (body %s)", recorder.Code, recorder.Body.String())
	}

	// Once the rotation is finished the same token is refused.
	withoutWindow := newProtectedEngine(t, newAuthTestService(t, repository, clock))
	if recorder := perform(withoutWindow, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer " + rotated,
	}); recorder.Code != http.StatusUnauthorized {
		t.Errorf("status after the rotation = %d, want 401", recorder.Code)
	}
}

func TestRequireAuthRejectsMissingAndInvalidCredentials(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.Nil, "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	clock := newServerTestClock()
	service := newAuthTestService(t, repository, clock)
	engine := newProtectedEngine(t, service)

	cases := map[string]map[string]string{
		"no header":    nil,
		"empty header": {AuthorizationHeader: ""},
		"scheme only":  {AuthorizationHeader: "Bearer"},
		"other scheme": {AuthorizationHeader: "Basic abc"},
		"bare token":   {AuthorizationHeader: "abc.def.ghi"},
		"junk token":   {AuthorizationHeader: "Bearer not-a-token"},
		"unknown key":  {AuthorizationHeader: "Bearer " + signTestToken(t, clock, testJWTUnknownSecret, uuid.NewString(), user)},
		"expired":      {AuthorizationHeader: "Bearer " + signExpiredTestToken(t, clock, uuid.NewString(), user)},
	}

	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := perform(engine, http.MethodGet, "/protected", headers)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", recorder.Code, recorder.Body.String())
			}
			if challenge := recorder.Header().Get("WWW-Authenticate"); !strings.HasPrefix(challenge, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a bearer challenge", challenge)
			}
			if body := decodeError(t, recorder); body.Code != CodeUnauthorized {
				t.Errorf("error code = %q, want %q", body.Code, CodeUnauthorized)
			}
			for _, secret := range []string{testJWTUnknownSecret, testJWTCurrentSecret, testUserPassword, user.PasswordHash} {
				if strings.Contains(recorder.Body.String(), secret) {
					t.Error("an authentication response must never echo a credential")
				}
			}
		})
	}
}

func TestRequireRoleEnforcesTheRole(t *testing.T) {
	tenantID := uuid.New()
	repository := &authTestRepository{}
	admin := newTestAuthUser(t, tenantID, uuid.Nil, "admin@example.com", auth.RoleStoreAdmin)
	super := newTestAuthUser(t, uuid.Nil, uuid.Nil, "root@example.com", auth.RoleSuperAdmin)
	repository.seed(admin)
	repository.seed(super)
	service := newAuthTestService(t, repository, newServerTestClock())

	anyAdminEngine := newProtectedEngine(t, service, auth.RoleStoreAdmin, auth.RoleSuperAdmin)
	superOnlyEngine := newProtectedEngine(t, service, auth.RoleSuperAdmin)

	adminToken := loginThroughService(t, service, admin.Email).Tokens.AccessToken
	superToken := loginThroughService(t, service, super.Email).Tokens.AccessToken

	// An allowed role passes.
	if recorder := perform(anyAdminEngine, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer " + adminToken,
	}); recorder.Code != http.StatusOK {
		t.Fatalf("allowed role: status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	// A disallowed role is 403, never 401.
	recorder := perform(superOnlyEngine, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer " + adminToken,
	})
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("disallowed role: status = %d, want 403 (body %s)", recorder.Code, recorder.Body.String())
	}
	if body := decodeError(t, recorder); body.Code != CodeForbidden {
		t.Errorf("error code = %q, want %q", body.Code, CodeForbidden)
	}

	// The platform account passes the super admin gate.
	if recorder := perform(superOnlyEngine, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer " + superToken,
	}); recorder.Code != http.StatusOK {
		t.Errorf("super admin: status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	// Without a principal the role gate answers 401, never 403.
	if recorder := perform(superOnlyEngine, http.MethodGet, "/protected", nil); recorder.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: status = %d, want 401", recorder.Code)
	}
}

func TestRequireAuthFailsClosedWithoutAService(t *testing.T) {
	engine := newProtectedEngine(t, nil)
	recorder := perform(engine, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer some.token.value",
	})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when authentication is not configured", recorder.Code)
	}
	if body := decodeError(t, recorder); body.Code != CodeInternalError {
		t.Errorf("error code = %q, want %q", body.Code, CodeInternalError)
	}
}

func TestRequireAuthReportsADependencyFailureAsInternalError(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.Nil, "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	service := newAuthTestService(t, repository, newServerTestClock())
	engine := newProtectedEngine(t, service)

	login := loginThroughService(t, service, user.Email)
	repository.failWith(errTestDatabaseDown)

	recorder := perform(engine, http.MethodGet, "/protected", map[string]string{
		AuthorizationHeader: "Bearer " + login.Tokens.AccessToken,
	})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for a dependency failure (a 401 would make the client drop a valid token)", recorder.Code)
	}
}

func TestPrincipalHelpersWithoutAnAuthenticatedCaller(t *testing.T) {
	if PrincipalOf(nil) != nil {
		t.Error("PrincipalOf(nil) must be nil")
	}
	if _, ok := ScopeOf(nil); ok {
		t.Error("ScopeOf(nil) must report no scope")
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/plain", func(c *gin.Context) {
		if PrincipalOf(c) != nil {
			c.String(http.StatusInternalServerError, "unexpected principal")
			return
		}
		if _, ok := ScopeOf(c); ok {
			c.String(http.StatusInternalServerError, "unexpected scope")
			return
		}
		c.String(http.StatusOK, "ok")
	})
	if recorder := perform(engine, http.MethodGet, "/plain", nil); recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for an unauthenticated route", recorder.Code)
	}

	// A platform account has no tenant scope: the helper fails closed.
	platform := &auth.Principal{User: newTestAuthUser(t, uuid.Nil, uuid.Nil, "root@example.com", auth.RoleSuperAdmin)}
	if _, err := platform.Scope(); err == nil {
		t.Error("a platform account must not yield a tenant scope")
	}
}
