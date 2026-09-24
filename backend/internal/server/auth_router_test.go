package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/auth"
	"github.com/m2sound2456/staffdisplay/backend/internal/logger"
)

// performJSON issues a request with a JSON body.
func performJSON(handler http.Handler, method, target string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var payload []byte
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		payload = raw
	}

	request := httptest.NewRequest(method, target, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// bearer builds the Authorization header of a token.
func bearer(token string) map[string]string {
	return map[string]string{AuthorizationHeader: "Bearer " + token}
}

// tokenPair decodes the "tokens" object of a login/refresh envelope.
func tokenPair(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	data := decodeData(t, recorder)
	tokens, ok := data["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("response has no tokens object: %s", recorder.Body.String())
	}
	for _, field := range []string{"access_token", "refresh_token", "token_type", "expires_in", "refresh_expires_in"} {
		if _, present := tokens[field]; !present {
			t.Errorf("tokens object is missing %q: %s", field, recorder.Body.String())
		}
	}
	if tokens["token_type"] != auth.TokenTypeBearer {
		t.Errorf("token_type = %v, want %q", tokens["token_type"], auth.TokenTypeBearer)
	}
	return tokens
}

func TestAuthLoginReturnsASessionForValidCredentials(t *testing.T) {
	tenantID, storeID := uuid.New(), uuid.New()
	repository := &authTestRepository{}
	user := newTestAuthUser(t, tenantID, storeID, "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	router := newAuthTestRouter(t, repository, newServerTestClock())

	recorder := performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    " Admin@Example.com ",
		"password": testUserPassword,
	}, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	data := decodeData(t, recorder)
	account, ok := data["user"].(map[string]any)
	if !ok {
		t.Fatalf("response has no user object: %s", recorder.Body.String())
	}
	if account["id"] != user.ID.String() {
		t.Errorf("user id = %v, want %s", account["id"], user.ID)
	}
	if account["role"] != string(auth.RoleStoreAdmin) || account["status"] != string(auth.StatusActive) {
		t.Errorf("account = %v, want the role and status of the account", account)
	}
	if account["tenant_id"] != tenantID.String() || account["store_id"] != storeID.String() {
		t.Errorf("scope = (%v, %v), want (%s, %s)", account["tenant_id"], account["store_id"], tenantID, storeID)
	}

	tokens := tokenPair(t, recorder)
	accessToken, _ := tokens["access_token"].(string)

	// The issued access token authenticates the caller.
	me := perform(router, http.MethodGet, "/api/v1/auth/me", bearer(accessToken))
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200 (body %s)", me.Code, me.Body.String())
	}
	if data := decodeData(t, me); data["id"] != user.ID.String() {
		t.Errorf("me id = %v, want %s", data["id"], user.ID)
	}

	// No credential may appear in the response.
	for _, secret := range []string{testUserPassword, user.PasswordHash, testJWTCurrentSecret} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Error("a login response must never contain a password, hash or signing key")
		}
	}
	if strings.Contains(recorder.Body.String(), "password_hash") {
		t.Error("a login response must not expose a password hash field")
	}
}

func TestAuthLoginRejectsInvalidCredentials(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.Nil, "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	router := newAuthTestRouter(t, repository, newServerTestClock())

	cases := map[string]map[string]string{
		"wrong password":  {"email": user.Email, "password": "not-the-password-123"},
		"unknown account": {"email": "nobody@example.com", "password": testUserPassword},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := performJSON(router, http.MethodPost, "/api/v1/auth/login", payload, nil)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", recorder.Code, recorder.Body.String())
			}
			if body := decodeError(t, recorder); body.Code != CodeUnauthorized {
				t.Errorf("error code = %q, want %q", body.Code, CodeUnauthorized)
			}
			if strings.Contains(recorder.Body.String(), "access_token") || strings.Contains(recorder.Body.String(), "refresh_token") {
				t.Error("a rejected login must not return tokens")
			}
			if strings.Contains(recorder.Body.String(), testUserPassword) {
				t.Error("a rejected login must not echo the password")
			}
		})
	}
}

func TestAuthLoginRejectsADisabledAccount(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.Nil, "admin@example.com", auth.RoleStoreAdmin)
	user.Status = auth.StatusDisabled
	repository.seed(user)
	router := newAuthTestRouter(t, repository, newServerTestClock())

	recorder := performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    user.Email,
		"password": testUserPassword,
	}, nil)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a disabled account (body %s)", recorder.Code, recorder.Body.String())
	}
	if body := decodeError(t, recorder); body.Code != CodeForbidden {
		t.Errorf("error code = %q, want %q", body.Code, CodeForbidden)
	}
}

func TestAuthLoginValidatesThePayload(t *testing.T) {
	router := newAuthTestRouter(t, &authTestRepository{}, newServerTestClock())

	// Missing fields → 422 with field level details.
	recorder := performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "  "}, nil)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body %s)", recorder.Code, recorder.Body.String())
	}
	body := decodeError(t, recorder)
	if body.Code != CodeValidationFailed {
		t.Errorf("error code = %q, want %q", body.Code, CodeValidationFailed)
	}
	details, ok := body.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %#v, want field level details", body.Details)
	}
	if details["email"] == nil || details["password"] == nil {
		t.Errorf("details = %v, want email and password", details)
	}

	// A malformed body → 400.
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{not json"))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: status = %d, want 400 (body %s)", recorder.Code, recorder.Body.String())
	}
	if body := decodeError(t, recorder); body.Code != CodeBadRequest {
		t.Errorf("error code = %q, want %q", body.Code, CodeBadRequest)
	}
}

func TestAuthMeRequiresAuthentication(t *testing.T) {
	router := newAuthTestRouter(t, &authTestRepository{}, newServerTestClock())

	for name, headers := range map[string]map[string]string{
		"no header": nil,
		"junk":      {AuthorizationHeader: "Bearer junk"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := perform(router, http.MethodGet, "/api/v1/auth/me", headers)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", recorder.Code, recorder.Body.String())
			}
			if body := decodeError(t, recorder); body.Code != CodeUnauthorized {
				t.Errorf("error code = %q, want %q", body.Code, CodeUnauthorized)
			}
		})
	}
}

func TestAuthMeIgnoresClientSuppliedScopeParameters(t *testing.T) {
	tenantID, storeID := uuid.New(), uuid.New()
	otherTenant, otherStore := uuid.New(), uuid.New()
	repository := &authTestRepository{}
	user := newTestAuthUser(t, tenantID, storeID, "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	router := newAuthTestRouter(t, repository, newServerTestClock())

	tokens := tokenPair(t, performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    user.Email,
		"password": testUserPassword,
	}, nil))
	accessToken, _ := tokens["access_token"].(string)

	// A caller cannot move itself into another tenant by naming ids in the
	// request: the scope comes from the account row (BLUEPRINT §3.2).
	target := "/api/v1/auth/me?tenant_id=" + otherTenant.String() + "&store_id=" + otherStore.String() + "&slug=other&role=super_admin"
	recorder := perform(router, http.MethodGet, target, bearer(accessToken))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	data := decodeData(t, recorder)
	if data["tenant_id"] != tenantID.String() || data["store_id"] != storeID.String() {
		t.Errorf("scope = (%v, %v), want the account scope (%s, %s)", data["tenant_id"], data["store_id"], tenantID, storeID)
	}
	if data["role"] != string(auth.RoleStoreAdmin) {
		t.Errorf("role = %v, want the stored role %q", data["role"], auth.RoleStoreAdmin)
	}
}

func TestAuthLogoutRevokesTheSession(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.New(), "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	router := newAuthTestRouter(t, repository, newServerTestClock())

	tokens := tokenPair(t, performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    user.Email,
		"password": testUserPassword,
	}, nil))
	accessToken, _ := tokens["access_token"].(string)

	if recorder := perform(router, http.MethodGet, "/api/v1/auth/me", bearer(accessToken)); recorder.Code != http.StatusOK {
		t.Fatalf("me before logout: status = %d, want 200", recorder.Code)
	}

	recorder := performJSON(router, http.MethodPost, "/api/v1/auth/logout", nil, bearer(accessToken))
	if recorder.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}
	if data := decodeData(t, recorder); data["logged_out"] != true {
		t.Errorf("logged_out = %v, want true", data["logged_out"])
	}

	// The credential is dead immediately, and a replay is rejected.
	if recorder := perform(router, http.MethodGet, "/api/v1/auth/me", bearer(accessToken)); recorder.Code != http.StatusUnauthorized {
		t.Errorf("me after logout: status = %d, want 401", recorder.Code)
	}
	if recorder := performJSON(router, http.MethodPost, "/api/v1/auth/logout", nil, bearer(accessToken)); recorder.Code != http.StatusUnauthorized {
		t.Errorf("repeated logout: status = %d, want 401", recorder.Code)
	}

	// Refreshing is refused as well: the session is gone.
	refreshToken, _ := tokens["refresh_token"].(string)
	if recorder := performJSON(router, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, nil); recorder.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout: status = %d, want 401", recorder.Code)
	}

	// Logout without credentials is a 401, not a silent success.
	if recorder := performJSON(router, http.MethodPost, "/api/v1/auth/logout", nil, nil); recorder.Code != http.StatusUnauthorized {
		t.Errorf("logout without credentials: status = %d, want 401", recorder.Code)
	}
}

func TestAuthRefreshRotatesTheCredential(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.New(), "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	clock := newServerTestClock()
	router := newAuthTestRouter(t, repository, clock)

	loginTokens := tokenPair(t, performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    user.Email,
		"password": testUserPassword,
	}, nil))
	refreshToken, _ := loginTokens["refresh_token"].(string)

	clock.advance(time.Minute)
	recorder := performJSON(router, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}
	rotated := tokenPair(t, recorder)
	if rotated["refresh_token"] == refreshToken {
		t.Error("refresh must rotate the refresh token")
	}
	if rotated["access_token"] == loginTokens["access_token"] {
		t.Error("refresh must issue a new access token")
	}

	// The new access token authenticates the caller.
	newAccess, _ := rotated["access_token"].(string)
	if recorder := perform(router, http.MethodGet, "/api/v1/auth/me", bearer(newAccess)); recorder.Code != http.StatusOK {
		t.Errorf("me with the refreshed token: status = %d, want 200", recorder.Code)
	}

	// The rotated-away refresh token can never be used again.
	if recorder := performJSON(router, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, nil); recorder.Code != http.StatusUnauthorized {
		t.Errorf("replayed refresh token: status = %d, want 401", recorder.Code)
	}

	// A missing or unknown token is refused.
	for name, payload := range map[string]map[string]string{
		"missing": {},
		"unknown": {"refresh_token": "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := performJSON(router, http.MethodPost, "/api/v1/auth/refresh", payload, nil)
			if recorder.Code != http.StatusUnauthorized && recorder.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 401/422 (body %s)", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "access_token") {
				t.Error("a rejected refresh must not return tokens")
			}
		})
	}
}

func TestAuthUsersListIsScopedByRole(t *testing.T) {
	tenantA, tenantB := uuid.New(), uuid.New()
	repository := &authTestRepository{}
	adminA := newTestAuthUser(t, tenantA, uuid.New(), "admin-a@example.com", auth.RoleStoreAdmin)
	colleagueA := newTestAuthUser(t, tenantA, uuid.Nil, "colleague-a@example.com", auth.RoleStoreAdmin)
	adminB := newTestAuthUser(t, tenantB, uuid.New(), "admin-b@example.com", auth.RoleStoreAdmin)
	super := newTestAuthUser(t, uuid.Nil, uuid.Nil, "root@example.com", auth.RoleSuperAdmin)
	for _, user := range []*auth.User{adminA, colleagueA, adminB, super} {
		repository.seed(user)
	}
	router := newAuthTestRouter(t, repository, newServerTestClock())

	loginAs := func(email string) string {
		tokens := tokenPair(t, performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"email":    email,
			"password": testUserPassword,
		}, nil))
		token, _ := tokens["access_token"].(string)
		return token
	}

	// A store admin sees its own tenant only: the tenant of tenant B never
	// appears, and the request carries no filter it could widen.
	storeAdminRecorder := perform(router, http.MethodGet, "/api/v1/auth/users", bearer(loginAs(adminA.Email)))
	if storeAdminRecorder.Code != http.StatusOK {
		t.Fatalf("store admin status = %d, want 200 (body %s)", storeAdminRecorder.Code, storeAdminRecorder.Body.String())
	}
	storeAdminData := decodeData(t, storeAdminRecorder)
	listed, _ := storeAdminData["users"].([]any)
	if len(listed) != 2 {
		t.Fatalf("store admin users = %d, want the 2 accounts of tenant A (body %s)", len(listed), storeAdminRecorder.Body.String())
	}
	for _, entry := range listed {
		account, _ := entry.(map[string]any)
		if account["tenant_id"] != tenantA.String() {
			t.Errorf("tenant %v leaked into the tenant A listing", account["tenant_id"])
		}
		if _, present := account["password_hash"]; present {
			t.Error("the account listing must not expose a password hash")
		}
	}

	// A platform super admin sees every tenant (BLUEPRINT §5).
	superRecorder := perform(router, http.MethodGet, "/api/v1/auth/users", bearer(loginAs(super.Email)))
	if superRecorder.Code != http.StatusOK {
		t.Fatalf("super admin status = %d, want 200 (body %s)", superRecorder.Code, superRecorder.Body.String())
	}
	superData := decodeData(t, superRecorder)
	if all, _ := superData["users"].([]any); len(all) != 4 {
		t.Errorf("super admin users = %d, want all 4 accounts", len(all))
	}

	// Unauthenticated access is refused.
	if recorder := perform(router, http.MethodGet, "/api/v1/auth/users", nil); recorder.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", recorder.Code)
	}
	if strings.Contains(superRecorder.Body.String(), super.PasswordHash) {
		t.Error("the listing must never contain a password hash")
	}
}

func TestAuthRoutesRejectTheWrongMethod(t *testing.T) {
	router := newAuthTestRouter(t, &authTestRepository{}, newServerTestClock())

	if recorder := perform(router, http.MethodGet, "/api/v1/auth/login", nil); recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /auth/login status = %d, want 405", recorder.Code)
	}
	if recorder := perform(router, http.MethodPost, "/api/v1/auth/me", nil); recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /auth/me status = %d, want 405", recorder.Code)
	}
}

// captureAccessLog runs fn while capturing everything the process-wide logger
// writes to stdout, and returns the captured text.
func captureAccessLog(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	original := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = original
		if err := logger.Init(logger.Options{Development: false, Level: "error", Encoding: "console"}); err != nil {
			t.Errorf("restore logger: %v", err)
		}
	})
	if err := logger.Init(logger.Options{Development: false, Level: "info", Encoding: "console"}); err != nil {
		t.Fatalf("logger.Init() error = %v", err)
	}

	captured := make(chan string, 1)
	go func() {
		content, _ := io.ReadAll(reader)
		captured <- string(content)
	}()

	fn()

	_ = writer.Close()
	os.Stdout = original
	return <-captured
}

func TestAuthAccessLogNeverContainsCredentials(t *testing.T) {
	repository := &authTestRepository{}
	user := newTestAuthUser(t, uuid.New(), uuid.New(), "admin@example.com", auth.RoleStoreAdmin)
	repository.seed(user)
	router := newAuthTestRouter(t, repository, newServerTestClock())

	var accessToken, refreshToken string
	log := captureAccessLog(t, func() {
		recorder := performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"email":    user.Email,
			"password": testUserPassword,
		}, nil)
		if recorder.Code != http.StatusOK {
			t.Errorf("login status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
		}
		tokens := tokenPair(t, recorder)
		accessToken, _ = tokens["access_token"].(string)
		refreshToken, _ = tokens["refresh_token"].(string)

		// A failing login is logged as well (401), and an authenticated call is
		// logged with its bearer header present in the request.
		performJSON(router, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"email":    user.Email,
			"password": "a-wrong-password-123",
		}, nil)
		perform(router, http.MethodGet, "/api/v1/auth/me", bearer(accessToken))
		performJSON(router, http.MethodPost, "/api/v1/auth/logout", nil, bearer(accessToken))
	})

	// The access log must have captured the requests (otherwise the assertions
	// below would be vacuous).
	if !strings.Contains(log, "/api/v1/auth/login") || !strings.Contains(log, "http_request") {
		t.Fatalf("the access log did not capture the requests: %s", log)
	}

	forbidden := map[string]string{
		"password":          testUserPassword,
		"password hash":     user.PasswordHash,
		"access token":      accessToken,
		"refresh token":     refreshToken,
		"signing key":       testJWTCurrentSecret,
		"previous key":      testJWTPreviousSecret,
		"authorization":     "Bearer " + accessToken,
		"authorization key": "Authorization",
	}
	for name, secret := range forbidden {
		if secret == "" {
			continue
		}
		if strings.Contains(log, secret) {
			t.Errorf("the access log contains the %s: %s", name, log)
		}
	}
}
