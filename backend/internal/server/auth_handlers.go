package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/m2sound2456/staffdisplay/backend/internal/auth"
)

// maxAuthCredentialBodyBytes bounds an authentication payload. A login or
// refresh body is a few hundred bytes, so an oversized one is rejected before it
// is parsed.
const maxAuthCredentialBodyBytes = 4096

// loginRequest is the payload of POST /auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// refreshRequest is the payload of POST /auth/refresh.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// userResponse is the public projection of an account. It is an explicit DTO so
// a new column can never leak through the API by accident — above all the
// password hash.
type userResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	TenantID    *string `json:"tenant_id"`
	StoreID     *string `json:"store_id"`
	LastLoginAt *string `json:"last_login_at"`
	CreatedAt   string  `json:"created_at"`
}

// tokensResponse carries the credential pair. It is never logged.
type tokensResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

// loginResponse is the payload of a successful login.
type loginResponse struct {
	User   userResponse   `json:"user"`
	Tokens tokensResponse `json:"tokens"`
}

// refreshResponse is the payload of a successful refresh.
type refreshResponse struct {
	Tokens tokensResponse `json:"tokens"`
}

// logoutResponse is the payload of a successful logout.
type logoutResponse struct {
	LoggedOut bool `json:"logged_out"`
}

// userListResponse is the payload of GET /auth/users.
type userListResponse struct {
	Users []userResponse `json:"users"`
	Count int            `json:"count"`
}

// login handles POST /api/v1/auth/login.
func (h *handlers) login(c *gin.Context) {
	var payload loginRequest
	if !bindCredentialBody(c, &payload) {
		return
	}

	result, err := h.authService.Login(c.Request.Context(), auth.LoginInput{
		Email:     payload.Email,
		Password:  payload.Password,
		UserAgent: c.Request.UserAgent(),
		IP:        c.ClientIP(),
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}

	OK(c, loginResponse{
		User:   newUserResponse(result.User),
		Tokens: newTokensResponse(result.Tokens),
	})
}

// refresh handles POST /api/v1/auth/refresh (refresh-token rotation).
func (h *handlers) refresh(c *gin.Context) {
	var payload refreshRequest
	if !bindCredentialBody(c, &payload) {
		return
	}

	result, err := h.authService.Refresh(c.Request.Context(), auth.RefreshInput{
		RefreshToken: payload.RefreshToken,
		UserAgent:    c.Request.UserAgent(),
		IP:           c.ClientIP(),
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}

	OK(c, refreshResponse{Tokens: newTokensResponse(result.Tokens)})
}

// logout handles POST /api/v1/auth/logout for the session behind the presented
// credential. The credential is dead afterwards, so a replay answers 401.
func (h *handlers) logout(c *gin.Context) {
	if err := h.authService.Logout(c.Request.Context(), PrincipalOf(c), c.ClientIP()); err != nil {
		writeAuthError(c, err)
		return
	}
	OK(c, logoutResponse{LoggedOut: true})
}

// me handles GET /api/v1/auth/me: the caller as the database describes it now.
// Any tenant_id/store_id query parameter is ignored on purpose — the scope of a
// caller is server controlled (BLUEPRINT §3.2).
func (h *handlers) me(c *gin.Context) {
	principal := PrincipalOf(c)
	if principal == nil || principal.User == nil {
		Unauthorized(c, "authentication required")
		return
	}
	OK(c, newUserResponse(principal.User))
}

// listUsers handles GET /api/v1/auth/users: the accounts the caller may see
// (its own tenant for a store admin, the platform for a super admin).
func (h *handlers) listUsers(c *gin.Context) {
	principal := PrincipalOf(c)
	if principal == nil {
		Unauthorized(c, "authentication required")
		return
	}

	users, err := h.authService.ListUsers(c.Request.Context(), principal, auth.ListFilter{
		Limit:  intQuery(c, "limit", 0),
		Offset: intQuery(c, "offset", 0),
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}

	list := make([]userResponse, 0, len(users))
	for index := range users {
		list = append(list, newUserResponse(&users[index]))
	}
	OK(c, userListResponse{Users: list, Count: len(list)})
}

// newUserResponse projects an account for the API.
func newUserResponse(user *auth.User) userResponse {
	if user == nil {
		return userResponse{}
	}
	response := userResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Role:        user.Role.String(),
		Status:      user.Status.String(),
		CreatedAt:   user.CreatedAt.UTC().Format(time.RFC3339),
	}
	if user.HasTenant() {
		tenant := user.TenantID.String()
		response.TenantID = &tenant
	}
	if user.HasStore() {
		store := user.StoreID.String()
		response.StoreID = &store
	}
	if user.LastLoginAt != nil {
		last := user.LastLoginAt.UTC().Format(time.RFC3339)
		response.LastLoginAt = &last
	}
	return response
}

// newTokensResponse projects a token pair (expiries as remaining seconds).
func newTokensResponse(tokens auth.Tokens) tokensResponse {
	return tokensResponse{
		AccessToken:      tokens.AccessToken,
		TokenType:        tokens.TokenType,
		ExpiresIn:        secondsUntil(tokens.AccessExpiresAt),
		RefreshToken:     tokens.RefreshToken,
		RefreshExpiresIn: secondsUntil(tokens.RefreshExpiresAt),
	}
}

// bindCredentialBody decodes a bounded JSON body, answering 400 for a malformed
// payload (a credential body is never echoed back).
func bindCredentialBody(c *gin.Context, target any) bool {
	body := http.MaxBytesReader(c.Writer, c.Request.Body, int64(maxAuthCredentialBodyBytes))
	if err := json.NewDecoder(body).Decode(target); err != nil {
		Fail(c, http.StatusBadRequest, CodeBadRequest, "request body must be a JSON object", nil)
		return false
	}
	return true
}

// intQuery parses a bounded, non-negative integer query parameter.
func intQuery(c *gin.Context, name string, fallback int) int {
	raw := c.Query(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

// secondsUntil renders a duration as whole seconds (never negative).
func secondsUntil(deadline time.Time) int64 {
	remaining := time.Until(deadline)
	if remaining < 0 {
		return 0
	}
	return int64(remaining.Seconds())
}

// writeAuthError maps an authentication error to the API envelope
// (docs/API.md §1). Driver and database details never reach the client: an
// unknown failure is reported as a generic internal error.
func writeAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth.ErrValidation):
		ValidationFailed(c, "request validation failed", validationDetails(err))
	case errors.Is(err, auth.ErrInvalidCredentials):
		Unauthorized(c, "invalid credentials")
	case errors.Is(err, auth.ErrUnauthenticated):
		Unauthorized(c, "authentication required")
	case errors.Is(err, auth.ErrAccountInactive):
		Forbidden(c, "account is not active")
	case errors.Is(err, auth.ErrForbidden):
		Forbidden(c, "")
	case errors.Is(err, auth.ErrEmailTaken), errors.Is(err, auth.ErrConflict):
		Fail(c, http.StatusConflict, CodeConflict, "the account could not be created", nil)
	case errors.Is(err, auth.ErrNotFound):
		NotFound(c, "")
	case errors.Is(err, auth.ErrNotConfigured):
		Fail(c, http.StatusInternalServerError, CodeInternalError, "authentication is not configured", nil)
	default:
		Fail(c, http.StatusInternalServerError, CodeInternalError, "authentication failed", nil)
	}
}

// fieldErrorer is implemented by the domain validation errors that expose their
// per-field problems (store and auth both do).
type fieldErrorer interface {
	FieldMessages() map[string]string
}

// validationDetails extracts the field level details of a validation error.
func validationDetails(err error) any {
	var detailed fieldErrorer
	if errors.As(err, &detailed) {
		return detailed.FieldMessages()
	}
	return nil
}
