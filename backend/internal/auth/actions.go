package auth

// Audit actions of this package (BLUEPRINT §11.10). Every privileged
// authentication action records exactly one entry, so the trail shows who signed
// in, when a token was rotated, and who signed out.
//
// The action names follow the dotted snake_case rule enforced by the
// audit_logs CHECK constraint; TestAuditActionsMatchPattern keeps them valid.
const (
	// ActionLoginSucceeded is recorded after a successful sign-in.
	ActionLoginSucceeded = "auth.login_succeeded"
	// ActionLoginFailed is recorded for a wrong password, an unknown address or
	// a disabled account. It never contains the attempted password.
	ActionLoginFailed = "auth.login_failed"
	// ActionLogout is recorded when a session is revoked by its owner.
	ActionLogout = "auth.logout"
	// ActionTokenRefreshed is recorded when a refresh token was rotated.
	ActionTokenRefreshed = "auth.token_refreshed"
	// ActionRefreshRejected is recorded when a refresh token was refused
	// (unknown, revoked, expired or reused after a rotation).
	ActionRefreshRejected = "auth.refresh_rejected"
)

// Metadata keys used by the entries above. Values are deliberately limited to
// the identifier and a fixed reason vocabulary — never a secret, token or
// password (docs/AI_RULES.md §5.7).
const (
	// MetadataKeyMethod names the authentication method ("password").
	MetadataKeyMethod = "method"
	// MetadataKeyReason carries a ReasonOf value for a rejection.
	MetadataKeyReason = "reason"
	// MetadataKeyEmail carries the attempted address of a failed login, so a
	// brute-force attempt can be investigated.
	MetadataKeyEmail = "email"
)

// LoginMethodPassword is the only authentication method FG4 supports. Device
// credentials (FG19) are a different method on a different actor.
const LoginMethodPassword = "password"

// AllAuditActions lists every action this package can record.
func AllAuditActions() []string {
	return []string{
		ActionLoginSucceeded,
		ActionLoginFailed,
		ActionLogout,
		ActionTokenRefreshed,
		ActionRefreshRejected,
	}
}
