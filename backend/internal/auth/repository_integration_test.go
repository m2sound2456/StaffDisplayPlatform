package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/auth"
	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/internal/store"
	"github.com/m2sound2456/staffdisplay/backend/internal/testsupport"
)

// testPassword satisfies the password policy and is never a real credential.
const testPassword = "correct-horse-battery-staple"

// setupPlatform migrates the test schema and creates two isolated tenants with
// one store each: the two sides of the isolation scenarios (BLUEPRINT §22).
func setupPlatform(t *testing.T) (db *database.Database, tenantA, tenantB, storeA, storeB uuid.UUID) {
	t.Helper()

	db = testsupport.Open(t)
	testsupport.MigrateSchema(t, db)

	tenantA = testsupport.NewTenant(t, db, "Tenant A")
	tenantB = testsupport.NewTenant(t, db, "Tenant B")
	storeA = testsupport.NewStore(t, db, tenantA, "Store A", "store-a")
	storeB = testsupport.NewStore(t, db, tenantB, "Store B", "store-b")
	return db, tenantA, tenantB, storeA, storeB
}

// mustHash hashes a test password through the production path.
func mustHash(t *testing.T, password string) string {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	return hash
}

func TestIntegrationFindUserByEmail(t *testing.T) {
	db, tenantA, _, storeA, _ := setupPlatform(t)
	repository := auth.NewRepository(db.Gorm())
	ctx := context.Background()

	email := "Admin-A@Example.com"
	userID := testsupport.NewUser(t, db, tenantA, storeA, email, testPassword, auth.RoleStoreAdmin)

	// The address is citext: a differently cased login resolves the same row.
	found, err := repository.FindUserByEmail(ctx, "admin-a@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail() error = %v", err)
	}
	if found.ID != userID {
		t.Errorf("user = %s, want %s", found.ID, userID)
	}
	if found.TenantIDValue() != tenantA || found.StoreIDValue() != storeA {
		t.Error("the loaded account must carry its tenant and store")
	}
	if !strings.HasPrefix(found.PasswordHash, "$2") {
		t.Errorf("password hash = %q, want a bcrypt digest", found.PasswordHash)
	}
	if !auth.VerifyPassword(found.PasswordHash, testPassword) {
		t.Error("the stored hash must verify the configured password")
	}

	if _, err := repository.FindUserByEmail(ctx, "nobody@example.com"); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("unknown address: error = %v, want ErrNotFound", err)
	}
	if _, err := repository.FindUserByEmail(ctx, "   "); !errors.Is(err, auth.ErrValidation) {
		t.Errorf("blank address: error = %v, want ErrValidation", err)
	}

	// A soft deleted account can no longer sign in and releases its address.
	if err := db.Gorm().WithContext(ctx).Exec("UPDATE users SET deleted_at = now() WHERE id = ?", userID).Error; err != nil {
		t.Fatalf("soft delete user: %v", err)
	}
	if _, err := repository.FindUserByEmail(ctx, email); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("soft deleted account: error = %v, want ErrNotFound", err)
	}
	if reused := testsupport.NewUser(t, db, tenantA, storeA, email, testPassword, auth.RoleStoreAdmin); reused == uuid.Nil {
		t.Fatal("a soft deleted address must be reusable")
	}
}

func TestIntegrationUserTenantIsolation(t *testing.T) {
	db, tenantA, tenantB, storeA, storeB := setupPlatform(t)
	repository := auth.NewRepository(db.Gorm())
	ctx := context.Background()

	userA := testsupport.NewUser(t, db, tenantA, storeA, "admin-a@example.com", testPassword, auth.RoleStoreAdmin)
	userB := testsupport.NewUser(t, db, tenantB, storeB, "admin-b@example.com", testPassword, auth.RoleStoreAdmin)

	// Tenant A reads a tenant B account: never disclosed, always not found.
	if _, err := repository.GetUser(ctx, store.TenantScope(tenantA), userB); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("cross-tenant GetUser() error = %v, want ErrNotFound", err)
	}
	own, err := repository.GetUser(ctx, store.TenantScope(tenantA), userA)
	if err != nil {
		t.Fatalf("GetUser(own) error = %v", err)
	}
	if own.ID != userA {
		t.Errorf("user = %s, want %s", own.ID, userA)
	}

	// A tenant scoped listing contains that tenant only.
	scoped, err := repository.ListUsers(ctx, store.TenantScope(tenantA), auth.ListFilter{})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != userA {
		t.Errorf("ListUsers() = %d rows, want only tenant A", len(scoped))
	}
	for _, user := range scoped {
		if user.TenantIDValue() != tenantA {
			t.Errorf("tenant %s leaked into the tenant A listing", user.TenantIDValue())
		}
	}

	// The platform scope (super admin) sees every tenant.
	platform, err := repository.ListPlatformUsers(ctx, auth.ListFilter{})
	if err != nil {
		t.Fatalf("ListPlatformUsers() error = %v", err)
	}
	if len(platform) != 2 {
		t.Errorf("ListPlatformUsers() = %d rows, want both tenants", len(platform))
	}

	// A store scoped query still carries the tenant condition.
	if _, err := repository.GetUser(ctx, store.StoreScope(tenantB, storeB), userA); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("cross-tenant store scope GetUser() error = %v, want ErrNotFound", err)
	}
}

func TestIntegrationCreateUser(t *testing.T) {
	db, tenantA, tenantB, storeA, _ := setupPlatform(t)
	repository := auth.NewRepository(db.Gorm())
	ctx := context.Background()

	// The scope wins over a tenant claimed by the model.
	user, err := auth.NewUser(store.TenantScope(tenantA), auth.NewUserInput{
		Email:        "claimed@example.com",
		DisplayName:  "Claimed",
		PasswordHash: mustHash(t, testPassword),
		Role:         auth.RoleStoreAdmin,
	})
	if err != nil {
		t.Fatalf("auth.NewUser() error = %v", err)
	}
	user.TenantID = &tenantB

	if err := repository.CreateUser(ctx, store.StoreScope(tenantA, storeA), user); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if user.TenantIDValue() != tenantA {
		t.Errorf("tenant = %s, want the scope tenant %s", user.TenantIDValue(), tenantA)
	}
	if user.StoreIDValue() != storeA {
		t.Errorf("store = %s, want the scope store %s", user.StoreIDValue(), storeA)
	}

	// A duplicate live address is reported as a conflict, not as a driver error.
	duplicate := &auth.User{
		Email:        "claimed@example.com",
		DisplayName:  "Duplicate",
		PasswordHash: mustHash(t, testPassword),
		Role:         auth.RoleStoreAdmin,
		Status:       auth.StatusActive,
	}
	if err := repository.CreateUser(ctx, store.TenantScope(tenantA), duplicate); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("duplicate address: error = %v, want ErrEmailTaken", err)
	}

	// A platform account needs no tenant.
	super := &auth.User{
		Email:        "root@example.com",
		DisplayName:  "Root",
		PasswordHash: mustHash(t, testPassword),
		Role:         auth.RoleSuperAdmin,
		Status:       auth.StatusActive,
	}
	if err := repository.CreateUser(ctx, store.Scope{}, super); err != nil {
		t.Fatalf("CreateUser(super admin) error = %v", err)
	}
	if super.HasTenant() {
		t.Error("a super admin must not carry a tenant")
	}
}

func TestIntegrationTouchLastLogin(t *testing.T) {
	db, tenantA, tenantB, storeA, _ := setupPlatform(t)
	repository := auth.NewRepository(db.Gorm())
	ctx := context.Background()

	userA := testsupport.NewUser(t, db, tenantA, storeA, "admin-a@example.com", testPassword, auth.RoleStoreAdmin)
	userB := testsupport.NewUser(t, db, tenantB, uuid.Nil, "admin-b@example.com", testPassword, auth.RoleStoreAdmin)

	stamp := time.Now().UTC().Truncate(time.Second)
	if err := repository.TouchLastLogin(ctx, store.TenantScope(tenantA), userA, stamp); err != nil {
		t.Fatalf("TouchLastLogin() error = %v", err)
	}
	if err := repository.TouchLastLogin(ctx, store.TenantScope(tenantA), userB, stamp); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("cross-tenant TouchLastLogin() error = %v, want ErrNotFound", err)
	}

	var lastLogin *time.Time
	if err := db.Gorm().WithContext(ctx).Raw("SELECT last_login_at FROM users WHERE id = ?", userA).Scan(&lastLogin).Error; err != nil {
		t.Fatalf("read last_login_at: %v", err)
	}
	if lastLogin == nil || !lastLogin.UTC().Truncate(time.Second).Equal(stamp) {
		t.Errorf("last_login_at = %v, want %v", lastLogin, stamp)
	}

	// The platform path is guarded by tenant_id IS NULL.
	superID := testsupport.NewUser(t, db, uuid.Nil, uuid.Nil, "root@example.com", testPassword, auth.RoleSuperAdmin)
	if err := repository.TouchPlatformLogin(ctx, superID, stamp); err != nil {
		t.Fatalf("TouchPlatformLogin() error = %v", err)
	}
	if err := repository.TouchPlatformLogin(ctx, userA, stamp); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("TouchPlatformLogin(tenant user) error = %v, want ErrNotFound", err)
	}
}

func TestIntegrationSessionLifecycle(t *testing.T) {
	db, tenantA, _, storeA, _ := setupPlatform(t)
	repository := auth.NewRepository(db.Gorm())
	ctx := context.Background()

	userID := testsupport.NewUser(t, db, tenantA, storeA, "admin@example.com", testPassword, auth.RoleStoreAdmin)
	user, err := repository.FindUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	refreshToken, digest, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	session, err := auth.NewSession(user, digest, 30*24*time.Hour, "test-agent", "203.0.113.7", now)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := repository.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// LoadSession returns the session together with its account.
	loaded, err := repository.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.User.ID != userID || loaded.Session.ID != session.ID {
		t.Error("LoadSession() must return the session and its account")
	}
	if loaded.Session.TenantIDValue() != tenantA {
		t.Error("the stored session must keep the tenant snapshot")
	}
	if loaded.Session.IP == nil || *loaded.Session.IP != "203.0.113.7" {
		t.Errorf("stored ip = %v, want the caller address", loaded.Session.IP)
	}

	// The digest resolves the session; a different value does not.
	if _, err := repository.FindSessionByRefreshDigest(ctx, digest); err != nil {
		t.Fatalf("FindSessionByRefreshDigest() error = %v", err)
	}
	if _, err := repository.FindSessionByRefreshDigest(ctx, auth.TokenDigest(auth.DigestToken("wrong-token"))); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("unknown digest: error = %v, want ErrSessionNotFound", err)
	}
	if refreshToken == string(digest) {
		t.Fatal("the stored digest must not be the token")
	}

	// A rotation replaces the digest: the old one stops resolving.
	rotatedToken, rotatedDigest, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	if err := repository.RotateSession(ctx, session.ID, rotatedDigest, now.Add(30*24*time.Hour), now); err != nil {
		t.Fatalf("RotateSession() error = %v", err)
	}
	if _, err := repository.FindSessionByRefreshDigest(ctx, digest); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("rotated-away digest: error = %v, want ErrSessionNotFound", err)
	}
	if _, err := repository.FindSessionByRefreshDigest(ctx, rotatedDigest); err != nil {
		t.Fatalf("new digest: error = %v", err)
	}
	if rotatedToken == refreshToken {
		t.Fatal("a rotation must produce a different token")
	}

	// Revocation is recorded once and stays visible afterwards.
	if err := repository.RevokeSession(ctx, session.ID, auth.RevocationLogout, now); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	revoked, err := repository.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() after revocation error = %v", err)
	}
	if !revoked.Session.IsRevoked() {
		t.Fatal("the session must be marked as revoked")
	}
	if reason := revoked.Session.RevokedReason; reason == nil || *reason != auth.RevocationLogout {
		t.Errorf("revoked reason = %v, want %q", reason, auth.RevocationLogout)
	}
	if err := repository.RevokeSession(ctx, session.ID, auth.RevocationLogout, now); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("second revocation: error = %v, want ErrSessionNotFound", err)
	}
	if err := repository.RotateSession(ctx, session.ID, rotatedDigest, now.Add(time.Hour), now); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("rotating a revoked session: error = %v, want ErrSessionNotFound", err)
	}
	if err := repository.RevokeSession(ctx, uuid.New(), auth.RevocationLogout, now); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("revoking an unknown session: error = %v, want ErrSessionNotFound", err)
	}
	if _, err := repository.LoadSession(ctx, uuid.New()); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("loading an unknown session: error = %v, want ErrSessionNotFound", err)
	}
}

func TestIntegrationDatabaseRejectsInvalidRows(t *testing.T) {
	db, tenantA, _, storeA, _ := setupPlatform(t)
	ctx := context.Background()
	gormDB := db.Gorm()

	run := func(t *testing.T, query string, args ...any) error {
		t.Helper()
		return gormDB.WithContext(ctx).Exec(query, args...).Error
	}
	validHash := mustHash(t, testPassword)
	insertUser := "INSERT INTO users (id, tenant_id, email, display_name, password_hash, role) VALUES (?, ?, ?, ?, ?, ?)"

	// A plaintext password can never reach the column: the CHECK mirrors the
	// domain rule, so the schema protects the invariant on its own.
	cases := []struct {
		name    string
		query   string
		args    []any
		wantErr bool
	}{
		{"plaintext password", insertUser, []any{uuid.New(), tenantA, "plain@example.com", "Plain", "supersecret", "store_admin"}, true},
		{"unknown role", insertUser, []any{uuid.New(), tenantA, "role@example.com", "Role", validHash, "owner"}, true},
		{"unknown status", "INSERT INTO users (id, tenant_id, email, display_name, password_hash, role, status) VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{uuid.New(), tenantA, "status@example.com", "Status", validHash, "store_admin", "suspended"}, true},
		{"super admin with a tenant", insertUser, []any{uuid.New(), tenantA, "root@example.com", "Root", validHash, "super_admin"}, true},
		{"store admin without a tenant", "INSERT INTO users (id, email, display_name, password_hash, role) VALUES (?, ?, ?, ?, ?)",
			[]any{uuid.New(), "orphan@example.com", "Orphan", validHash, "store_admin"}, true},
		{"malformed email", insertUser, []any{uuid.New(), tenantA, "not-an-address", "Bad", validHash, "store_admin"}, true},
		{"blank display name", insertUser, []any{uuid.New(), tenantA, "blank@example.com", "   ", validHash, "store_admin"}, true},
		{"unknown tenant", insertUser, []any{uuid.New(), uuid.New(), "foreign@example.com", "Foreign", validHash, "store_admin"}, true},
		{"valid tenant account", "INSERT INTO users (id, tenant_id, store_id, email, display_name, password_hash, role) VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{uuid.New(), tenantA, storeA, "valid@example.com", "Valid", validHash, "store_admin"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := run(t, tc.query, tc.args...)
			switch {
			case tc.wantErr && err == nil:
				t.Error("the database accepted a row the domain forbids")
			case !tc.wantErr && err != nil:
				t.Errorf("the database rejected a valid row: %v", err)
			}
		})
	}

	// A plaintext refresh token is refused as well.
	sessionUser := testsupport.NewUser(t, db, tenantA, storeA, "session@example.com", testPassword, auth.RoleStoreAdmin)
	err := run(t,
		"INSERT INTO user_sessions (id, user_id, refresh_token_hash, expires_at) VALUES (?, ?, ?, now() + interval '1 hour')",
		uuid.New(), sessionUser, "plaintext-token")
	if err == nil {
		t.Error("the database accepted a plaintext refresh token")
	}

	// A revocation without a reason is refused (the pair is atomic).
	atomicUser := testsupport.NewUser(t, db, tenantA, storeA, "atomic@example.com", testPassword, auth.RoleStoreAdmin)
	err = run(t,
		"INSERT INTO user_sessions (id, user_id, refresh_token_hash, expires_at, revoked_at) VALUES (?, ?, ?, now() + interval '1 hour', now())",
		uuid.New(), atomicUser, auth.DigestToken("token"))
	if err == nil {
		t.Error("the database accepted a revocation without a reason")
	}
}

func TestIntegrationTenantDeletionCascadesToAccounts(t *testing.T) {
	db, tenantA, tenantB, storeA, _ := setupPlatform(t)
	ctx := context.Background()
	repository := auth.NewRepository(db.Gorm())

	userA := testsupport.NewUser(t, db, tenantA, storeA, "admin-a@example.com", testPassword, auth.RoleStoreAdmin)
	userB := testsupport.NewUser(t, db, tenantB, uuid.Nil, "admin-b@example.com", testPassword, auth.RoleStoreAdmin)

	// A session of tenant A must disappear with its tenant.
	user, err := repository.FindUserByEmail(ctx, "admin-a@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail() error = %v", err)
	}
	_, digest, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	session, err := auth.NewSession(user, digest, time.Hour, "test-agent", "", time.Now().UTC())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := repository.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := db.Gorm().WithContext(ctx).Exec("DELETE FROM tenants WHERE id = ?", tenantA).Error; err != nil {
		t.Fatalf("delete tenant A: %v", err)
	}

	var sessions int64
	if err := db.Gorm().WithContext(ctx).Raw("SELECT count(*) FROM user_sessions WHERE id = ?", session.ID).Scan(&sessions).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Error("deleting a tenant must cascade to the sessions of its accounts")
	}

	if _, err := repository.GetUser(ctx, store.TenantScope(tenantA), userA); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("removed tenant account: error = %v, want ErrNotFound", err)
	}
	// Tenant B is untouched.
	if _, err := repository.GetUser(ctx, store.TenantScope(tenantB), userB); err != nil {
		t.Errorf("tenant B account error = %v, want it to survive", err)
	}
}

func TestIntegrationUserUpdatedAtTrigger(t *testing.T) {
	db, tenantA, _, storeA, _ := setupPlatform(t)
	ctx := context.Background()

	userID := testsupport.NewUser(t, db, tenantA, storeA, "trigger@example.com", testPassword, auth.RoleStoreAdmin)

	var before, after time.Time
	if err := db.Gorm().WithContext(ctx).Raw("SELECT updated_at FROM users WHERE id = ?", userID).Scan(&before).Error; err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := db.Gorm().WithContext(ctx).Exec("UPDATE users SET display_name = ? WHERE id = ?", "Renamed", userID).Error; err != nil {
		t.Fatalf("update user: %v", err)
	}
	if err := db.Gorm().WithContext(ctx).Raw("SELECT updated_at FROM users WHERE id = ?", userID).Scan(&after).Error; err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if !after.After(before) {
		t.Error("the set_updated_at() trigger must refresh users.updated_at")
	}
}
