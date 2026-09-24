package auth

import (
	"errors"
)

// Token reasons: the fixed vocabulary used in logs and audit metadata for a
// rejected credential. Values never contain token material.
const (
	// ReasonMissing marks a request without usable credentials.
	ReasonMissing = "missing"
	// ReasonMalformed marks a header or token that is not a well formed JWS.
	ReasonMalformed = "malformed"
	// ReasonSignature marks a signature no configured key could verify.
	ReasonSignature = "unknown_signing_key"
	// ReasonExpired marks an expired token or session.
	ReasonExpired = "expired"
	// ReasonNotYetValid marks a token used before its nbf.
	ReasonNotYetValid = "not_yet_valid"
	// ReasonClaims marks a token with unusable claims.
	ReasonClaims = "invalid_claims"
	// ReasonSessionMissing marks a token whose session no longer exists.
	ReasonSessionMissing = "session_missing"
	// ReasonSessionRevoked marks a logged out or revoked session.
	ReasonSessionRevoked = "session_revoked"
	// ReasonAccountInactive marks a disabled account.
	ReasonAccountInactive = "account_inactive"
	// ReasonInvalid marks anything else.
	ReasonInvalid = "invalid"
)

// ReasonOf maps an authentication error to its fixed vocabulary value, so a log
// line or an audit entry can describe a rejection without echoing a credential.
func ReasonOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTokenMalformed):
		return ReasonMalformed
	case errors.Is(err, ErrTokenSignature):
		return ReasonSignature
	case errors.Is(err, ErrTokenExpired), errors.Is(err, ErrSessionExpired):
		return ReasonExpired
	case errors.Is(err, ErrTokenNotYetValid):
		return ReasonNotYetValid
	case errors.Is(err, ErrTokenInvalid):
		return ReasonClaims
	case errors.Is(err, ErrSessionNotFound):
		return ReasonSessionMissing
	case errors.Is(err, ErrSessionRevoked):
		return ReasonSessionRevoked
	case errors.Is(err, ErrAccountInactive):
		return ReasonAccountInactive
	case errors.Is(err, ErrUnauthenticated), errors.Is(err, ErrInvalidCredentials):
		return ReasonMissing
	default:
		return ReasonInvalid
	}
}
