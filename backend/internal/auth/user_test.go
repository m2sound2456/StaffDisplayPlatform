package auth

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/store"
	"github.com/m2sound2456/staffdisplay/backend/migrations"
)

// migrationSQL reads a shipped migration so a Go rule can be compared with the
// constraint the database enforces.
func migrationSQL(t *testing.T, name string) string {
	t.Helper()

	content, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func TestRoleAndStatusValues(t *testing.T) {
	if !RoleSuperAdmin.Valid() || !RoleStoreAdmin.Valid() {
		t.Error("the documented roles must validate")
	}
	if Role("owner").Valid() || Role("").Valid() {
		t.Error("an undocumented or empty role must not validate")
	}
	if !RoleSuperAdmin.IsPlatform() || RoleStoreAdmin.IsPlatform() {
		t.Error("only super_admin is platform scoped")
	}
	if got := RoleNames(); !strings.Contains(got, string(RoleSuperAdmin)) || !strings.Contains(got, string(RoleStoreAdmin)) {
		t.Errorf("RoleNames() = %q, want both roles", got)
	}

	if !StatusActive.Valid() || !StatusDisabled.Valid() {
		t.Error("the documented statuses must validate")
	}
	if Status("suspended").Valid() {
		t.Error("an undocumented status must not validate")
	}
	if got := StatusNames(); !strings.Contains(got, string(StatusActive)) {
		t.Errorf("StatusNames() = %q, want the active status", got)
	}
	if RoleStoreAdmin.String() != "store_admin" || StatusDisabled.String() != "disabled" {
		t.Error("String() must render the stored value")
	}
}

func TestNewUserBuildsScopedAccounts(t *testing.T) {
	tenantID, storeID := uuid.New(), uuid.New()

	storeAdmin, err := NewUser(store.TenantScope(tenantID), NewUserInput{
		Email:        "  Admin@Example.COM ",
		DisplayName:  "  Store Admin  ",
		PasswordHash: testPasswordHash(),
		Role:         RoleStoreAdmin,
	})
	if err != nil {
		t.Fatalf("NewUser() error = %v", err)
	}
	if storeAdmin.Email != "admin@example.com" {
		t.Errorf("email = %q, want the normalised address", storeAdmin.Email)
	}
	if storeAdmin.DisplayName != "Store Admin" {
		t.Errorf("display name = %q, want the trimmed value", storeAdmin.DisplayName)
	}
	if storeAdmin.Status != StatusActive {
		t.Errorf("status = %q, want the default %q", storeAdmin.Status, StatusActive)
	}
	if storeAdmin.TenantIDValue() != tenantID {
		t.Errorf("tenant = %s, want %s", storeAdmin.TenantIDValue(), tenantID)
	}
	if storeAdmin.HasStore() {
		t.Error("a tenant scope without a store must not bind one")
	}

	scoped, err := NewUser(store.StoreScope(tenantID, storeID), NewUserInput{
		Email:        "second@example.com",
		DisplayName:  "Second",
		PasswordHash: testPasswordHash(),
		Role:         RoleStoreAdmin,
	})
	if err != nil {
		t.Fatalf("NewUser() error = %v", err)
	}
	if scoped.StoreIDValue() != storeID {
		t.Errorf("store = %s, want %s", scoped.StoreIDValue(), storeID)
	}

	super, err := NewUser(store.Scope{}, NewUserInput{
		Email:        "root@example.com",
		DisplayName:  "Root",
		PasswordHash: testPasswordHash(),
		Role:         RoleSuperAdmin,
	})
	if err != nil {
		t.Fatalf("NewUser(super admin) error = %v", err)
	}
	if super.HasTenant() || super.HasStore() {
		t.Error("a super admin must not carry a tenant or a store")
	}
	if _, err := super.Scope(); err == nil {
		t.Error("a platform account must have no tenant scope")
	}
}

func TestNewUserRejectsAnInconsistentScope(t *testing.T) {
	tenantID := uuid.New()

	cases := []struct {
		name  string
		scope store.Scope
		input NewUserInput
	}{
		{
			name:  "store admin without a tenant",
			scope: store.Scope{},
			input: NewUserInput{Email: "a@example.com", DisplayName: "A", PasswordHash: testPasswordHash(), Role: RoleStoreAdmin},
		},
		{
			name:  "super admin with a tenant",
			scope: store.TenantScope(tenantID),
			input: NewUserInput{Email: "b@example.com", DisplayName: "B", PasswordHash: testPasswordHash(), Role: RoleSuperAdmin},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewUser(tc.scope, tc.input); err == nil {
				t.Fatal("NewUser() = nil error, want a validation error")
			}
		})
	}
}

func TestUserScopeFailsClosedWithoutATenant(t *testing.T) {
	super := &User{ID: uuid.New(), Role: RoleSuperAdmin, Status: StatusActive}
	if _, err := super.Scope(); !errors.Is(err, ErrMissingTenantScope) {
		t.Fatalf("Scope() error = %v, want ErrMissingTenantScope", err)
	}

	tenantID, storeID := uuid.New(), uuid.New()
	scoped := &User{ID: uuid.New(), TenantID: cloneUUID(&tenantID), StoreID: cloneUUID(&storeID)}
	scope, err := scoped.Scope()
	if err != nil {
		t.Fatalf("Scope() error = %v", err)
	}
	if scope.TenantID != tenantID || scope.StoreID != storeID {
		t.Errorf("scope = %v, want tenant %s / store %s", scope, tenantID, storeID)
	}

	tenantOnly := &User{ID: uuid.New(), TenantID: cloneUUID(&tenantID)}
	tenantScope, err := tenantOnly.Scope()
	if err != nil {
		t.Fatalf("Scope() error = %v", err)
	}
	if tenantScope.TenantID != tenantID || tenantScope.HasStore() {
		t.Errorf("scope = %v, want a tenant scope without a store", tenantScope)
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Admin@Example.COM "); got != "admin@example.com" {
		t.Errorf("NormalizeEmail() = %q, want admin@example.com", got)
	}
	if got := NormalizeEmail("   "); got != "" {
		t.Errorf("NormalizeEmail() = %q, want an empty string", got)
	}
}

func TestUserHelpers(t *testing.T) {
	tenantID := uuid.New()
	user := newTestUser(t, tenantID, uuid.Nil, RoleStoreAdmin)
	if !user.IsActive() || user.IsDeleted() || !user.HasTenant() || user.HasStore() {
		t.Error("an active tenant account must report IsActive/HasTenant without a store")
	}
	if user.StoreIDValue() != uuid.Nil {
		t.Error("StoreIDValue() must be the nil UUID without a store")
	}
	user.Status = StatusDisabled
	if user.IsActive() {
		t.Error("a disabled account must not be active")
	}
}

func TestUserValidateMirrorsTheDatabaseConstraints(t *testing.T) {
	tenantID := uuid.New()

	valid := func() *User {
		return &User{
			ID:           uuid.New(),
			TenantID:     cloneUUID(&tenantID),
			Email:        "admin@example.com",
			DisplayName:  "Store Admin",
			PasswordHash: testPasswordHash(),
			Role:         RoleStoreAdmin,
			Status:       StatusActive,
		}
	}

	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate() error = %v for a valid account", err)
	}

	cases := []struct {
		name   string
		mutate func(*User)
		field  string
	}{
		{"missing id", func(u *User) { u.ID = uuid.Nil }, "id"},
		{"missing email", func(u *User) { u.Email = "" }, "email"},
		{"malformed email", func(u *User) { u.Email = "not-an-address" }, "email"},
		{"email without a dot", func(u *User) { u.Email = "admin@example" }, "email"},
		{"email with a space", func(u *User) { u.Email = "ad min@example.com" }, "email"},
		{"email too long", func(u *User) { u.Email = strings.Repeat("a", MaxEmailLength) + "@example.com" }, "email"},
		{"blank display name", func(u *User) { u.DisplayName = "   " }, "display_name"},
		{"display name too long", func(u *User) { u.DisplayName = strings.Repeat("x", MaxDisplayNameLength+1) }, "display_name"},
		{"plaintext password", func(u *User) { u.PasswordHash = strongPassword }, "password_hash"},
		{"empty password hash", func(u *User) { u.PasswordHash = "" }, "password_hash"},
		{"unknown role", func(u *User) { u.Role = Role("owner") }, "role"},
		{"unknown status", func(u *User) { u.Status = Status("suspended") }, "status"},
		{"store admin without a tenant", func(u *User) { u.TenantID = nil }, "tenant_id"},
		{"super admin with a tenant", func(u *User) { u.Role = RoleSuperAdmin }, "role"},
		{"store without a tenant", func(u *User) { u.TenantID = nil; u.StoreID = cloneUUID(&tenantID) }, "tenant_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := valid()
			tc.mutate(user)

			err := user.Validate()
			if err == nil {
				t.Fatal("Validate() = nil error, want a validation error")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("Validate() error = %v, want ErrValidation", err)
			}
			var detailed *ValidationError
			if !errors.As(err, &detailed) {
				t.Fatalf("Validate() error = %v, want a field level error", err)
			}
			if detailed.FieldMessages()[tc.field] == "" {
				t.Errorf("field details = %v, want a message for %q", detailed.FieldMessages(), tc.field)
			}
		})
	}
}

func TestUserNormalizeCanonicalisesAndDropsAStrayStore(t *testing.T) {
	storeID := uuid.New()
	user := &User{
		Email:       "  Admin@Example.COM ",
		DisplayName: "  Admin  ",
		Role:        RoleStoreAdmin,
		StoreID:     cloneUUID(&storeID),
	}
	user.Normalize()

	if user.Email != "admin@example.com" || user.DisplayName != "Admin" {
		t.Errorf("Normalize() = (%q, %q), want the canonical form", user.Email, user.DisplayName)
	}
	if user.Status != StatusActive {
		t.Errorf("status = %q, want the default", user.Status)
	}
	// A store without a tenant cannot be stored (the schema requires the pair).
	if user.HasStore() {
		t.Error("a store must be dropped when the account has no tenant")
	}
}

func TestEmailRulesMatchDatabaseConstraint(t *testing.T) {
	sql := migrationSQL(t, "0005_create_users.sql")

	if !strings.Contains(sql, EmailPattern) {
		t.Errorf("0005_create_users.sql must enforce the e-mail pattern %q", EmailPattern)
	}
	for _, role := range AllRoles() {
		if !strings.Contains(sql, "'"+string(role)+"'") {
			t.Errorf("0005_create_users.sql must allow the role %q", role)
		}
	}
	for _, status := range AllStatuses() {
		if !strings.Contains(sql, "'"+string(status)+"'") {
			t.Errorf("0005_create_users.sql must allow the status %q", status)
		}
	}
	// The bcrypt rule keeps plaintext out of the column; the Go guard must
	// recognise exactly the same digest shape.
	if !strings.Contains(sql, `^[$]2[aby][$][0-9]{2}[$][./A-Za-z0-9]{53}$`) {
		t.Error("0005_create_users.sql must pin the bcrypt digest shape of users.password_hash")
	}
	if !strings.Contains(sql, "WHERE deleted_at IS NULL") {
		t.Error("0005_create_users.sql must keep the partial indexes over live rows")
	}
	for _, name := range []string{"users_email_key", "users_tenant_id_idx", "trg_users_updated_at"} {
		if !strings.Contains(sql, name) {
			t.Errorf("0005_create_users.sql must define %s", name)
		}
	}
}
