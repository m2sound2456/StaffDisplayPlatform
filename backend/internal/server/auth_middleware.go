package server

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/m2sound2456/staffdisplay/backend/internal/auth"
	"github.com/m2sound2456/staffdisplay/backend/internal/store"
)

// AuthorizationHeader is the request header carrying the bearer credential
// (docs/API.md §1). The value is never logged.
const AuthorizationHeader = "Authorization"

// principalKey is the gin context key holding the authenticated caller.
const principalKey = "auth_principal"

// authChallenge is the WWW-Authenticate value of a rejected credential.
const authChallenge = `Bearer realm="staffdisplay"`

// RequireAuth validates the bearer credential of a request, resolves the caller
// and stores the principal on the context.
//
// The identity (role, tenant, store, status) is read from the database on every
// request, so revoking a session, disabling an account or changing a role takes
// effect immediately — a token can never carry an authorization that the
// database no longer grants.
func RequireAuth(service *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := auth.ParseBearer(c.GetHeader(AuthorizationHeader))
		if err != nil {
			rejectAuthentication(c, err)
			return
		}

		principal, err := service.Authenticate(c.Request.Context(), token)
		if err != nil {
			rejectAuthentication(c, err)
			return
		}

		c.Set(principalKey, principal)
		c.Next()
	}
}

// RequireRole allows only the given roles. It must run after RequireAuth.
func RequireRole(roles ...auth.Role) gin.HandlerFunc {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, string(role))
	}

	return func(c *gin.Context) {
		principal := PrincipalOf(c)
		if principal == nil {
			rejectAuthentication(c, auth.ErrUnauthenticated)
			return
		}
		if !principal.HasRole(roles...) {
			// The caller is authenticated but not authorized: 403 with the roles
			// that would be needed (never with what the caller holds).
			Fail(c, http.StatusForbidden, CodeForbidden, "insufficient role",
				gin.H{"required_roles": names})
			return
		}
		c.Next()
	}
}

// PrincipalOf returns the authenticated caller of the request, or nil.
func PrincipalOf(c *gin.Context) *auth.Principal {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(principalKey); ok {
		if principal, ok := value.(*auth.Principal); ok {
			return principal
		}
	}
	return nil
}

// ScopeOf returns the tenant isolation scope of the authenticated caller. It is
// the only scope a feature group may pass to a repository: it comes from the
// caller's account, never from the request (BLUEPRINT §3.2).
func ScopeOf(c *gin.Context) (store.Scope, bool) {
	principal := PrincipalOf(c)
	if principal == nil {
		return store.Scope{}, false
	}
	scope, err := principal.Scope()
	if err != nil {
		return store.Scope{}, false
	}
	return scope, true
}

// rejectAuthentication answers a request whose credential is missing, invalid or
// no longer usable. The response never discloses which of these it was beyond
// "you are not authenticated", and the credential itself is not echoed.
func rejectAuthentication(c *gin.Context, err error) {
	c.Header("WWW-Authenticate", authChallenge)

	switch {
	case errors.Is(err, auth.ErrNotConfigured):
		Fail(c, http.StatusInternalServerError, CodeInternalError, "authentication is not configured", nil)
	case errors.Is(err, auth.ErrUnauthenticated):
		Unauthorized(c, "authentication required")
	case isCredentialRejection(err):
		Unauthorized(c, "invalid or expired credentials")
	default:
		// A dependency failure (database) must not be reported as a bad
		// credential: the caller would retry with the same, valid token.
		Fail(c, http.StatusInternalServerError, CodeInternalError, "authentication failed", nil)
	}
}

// isCredentialRejection reports whether an error is a definite rejection of the
// presented credential (as opposed to a dependency failure).
func isCredentialRejection(err error) bool {
	sentinels := []error{
		auth.ErrTokenMalformed,
		auth.ErrTokenSignature,
		auth.ErrTokenExpired,
		auth.ErrTokenNotYetValid,
		auth.ErrTokenInvalid,
		auth.ErrSessionNotFound,
		auth.ErrSessionRevoked,
		auth.ErrSessionExpired,
		auth.ErrAccountInactive,
	}
	for _, sentinel := range sentinels {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}
