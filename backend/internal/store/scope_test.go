package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newDryRunDB builds a GORM handle that renders SQL without connecting, so the
// scope helpers can be asserted without PostgreSQL.
func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=5432 user=test dbname=test sslmode=disable"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatalf("build dry-run database handle: %v", err)
	}
	return db
}

// renderedSQL returns the SQL GORM would execute for a query builder.
func renderedSQL(t *testing.T, query *gorm.DB, model any) (string, []any) {
	t.Helper()

	result := query.Find(model)
	if result.Error != nil {
		t.Fatalf("render SQL: %v", result.Error)
	}
	return result.Statement.SQL.String(), result.Statement.Vars
}

func containsUUID(values []any, want uuid.UUID) bool {
	for _, value := range values {
		switch typed := value.(type) {
		case uuid.UUID:
			if typed == want {
				return true
			}
		case *uuid.UUID:
			if typed != nil && *typed == want {
				return true
			}
		default:
			if fmt.Sprint(value) == want.String() {
				return true
			}
		}
	}
	return false
}

func TestScopeBuilders(t *testing.T) {
	tenantID, storeID := uuid.New(), uuid.New()

	tenantScope := TenantScope(tenantID)
	if !tenantScope.HasTenant() || tenantScope.HasStore() || tenantScope.IsZero() {
		t.Errorf("TenantScope(%s) = %+v, want tenant only", tenantID, tenantScope)
	}

	storeScope := StoreScope(tenantID, storeID)
	if !storeScope.HasTenant() || !storeScope.HasStore() || storeScope.IsZero() {
		t.Errorf("StoreScope(%s, %s) = %+v, want tenant and store", tenantID, storeID, storeScope)
	}

	if !(Scope{}).IsZero() {
		t.Error("zero Scope must report IsZero")
	}
	if got := (Scope{}).String(); got != "scope(empty)" {
		t.Errorf("zero Scope String() = %q, want scope(empty)", got)
	}
	if got := storeScope.String(); !strings.Contains(got, tenantID.String()) || !strings.Contains(got, storeID.String()) {
		t.Errorf("storeScope String() = %q, want tenant and store ids", got)
	}
}

func TestTenantScopeNarrowsQueriesToTheTenant(t *testing.T) {
	db := newDryRunDB(t)
	tenantID := uuid.New()

	query := TenantScope(tenantID).Apply(db.Model(&Store{}))
	sql, vars := renderedSQL(t, query, &[]Store{})

	if !strings.Contains(sql, TenantColumn) {
		t.Errorf("tenant scoped SQL does not filter on %s: %s", TenantColumn, sql)
	}
	if strings.Contains(sql, StoreColumn) {
		t.Errorf("tenant scoped SQL must not filter on %s: %s", StoreColumn, sql)
	}
	if !containsUUID(vars, tenantID) {
		t.Errorf("tenant scoped SQL bound vars %v do not contain %s", vars, tenantID)
	}
	// Store uses gorm.DeletedAt: the isolation query must never see deleted rows.
	if !strings.Contains(sql, DeletedColumn) || !strings.Contains(sql, "IS NULL") {
		t.Errorf("store query must exclude soft deleted rows: %s", sql)
	}
}

func TestStoreScopeNarrowsQueriesToTenantAndStore(t *testing.T) {
	db := newDryRunDB(t)
	tenantID, storeID := uuid.New(), uuid.New()

	query := StoreScope(tenantID, storeID).Apply(db.Model(&Store{}))
	sql, vars := renderedSQL(t, query, &[]Store{})

	if !strings.Contains(sql, TenantColumn) || !strings.Contains(sql, StoreColumn) {
		t.Errorf("store scoped SQL must filter on %s and %s: %s", TenantColumn, StoreColumn, sql)
	}
	if !containsUUID(vars, tenantID) || !containsUUID(vars, storeID) {
		t.Errorf("store scoped SQL bound vars %v must contain tenant %s and store %s", vars, tenantID, storeID)
	}
}

func TestZeroScopeAddsNoCondition(t *testing.T) {
	db := newDryRunDB(t)

	query := (Scope{}).Apply(db.Model(&Store{}))
	sql, _ := renderedSQL(t, query, &[]Store{})

	// A zero scope is inert: the guard against unscoped business queries lives in
	// ensureTenantScope (repository layer), not in Apply.
	if strings.Contains(sql, TenantColumn+" =") || strings.Contains(sql, StoreColumn+" =") {
		t.Errorf("zero scope must not add an isolation condition: %s", sql)
	}
}

func TestScopeApplyToleratesNilHandle(t *testing.T) {
	if got := (Scope{TenantID: uuid.New()}).Apply(nil); got != nil {
		t.Errorf("Apply(nil) = %v, want nil", got)
	}
	if got := OnlyLive(nil); got != nil {
		t.Errorf("OnlyLive(nil) = %v, want nil", got)
	}
}

func TestStandaloneScopeHelpers(t *testing.T) {
	db := newDryRunDB(t)
	tenantID, storeID := uuid.New(), uuid.New()

	tenantSQL, _ := renderedSQL(t, ScopedToTenant(db.Model(&Store{}), tenantID), &[]Store{})
	if !strings.Contains(tenantSQL, TenantColumn) {
		t.Errorf("ScopedToTenant must filter on %s: %s", TenantColumn, tenantSQL)
	}

	storeSQL, vars := renderedSQL(t, ScopedToStore(db.Model(&Store{}), tenantID, storeID), &[]Store{})
	if !strings.Contains(storeSQL, TenantColumn) || !strings.Contains(storeSQL, StoreColumn) {
		t.Errorf("ScopedToStore must filter on tenant and store: %s", storeSQL)
	}
	if !containsUUID(vars, tenantID) || !containsUUID(vars, storeID) {
		t.Errorf("ScopedToStore bound vars %v must contain tenant %s and store %s", vars, tenantID, storeID)
	}

	liveSQL, _ := renderedSQL(t, OnlyLive(db.Table("employees")), &[]map[string]any{})
	if !strings.Contains(liveSQL, DeletedColumn) || !strings.Contains(liveSQL, "IS NULL") {
		t.Errorf("OnlyLive must filter on %s IS NULL: %s", DeletedColumn, liveSQL)
	}
}

func TestEnsureTenantScopeRejectsMissingTenant(t *testing.T) {
	if err := ensureTenantScope(TenantScope(uuid.New())); err != nil {
		t.Errorf("ensureTenantScope(tenant scope) = %v, want nil", err)
	}
	if err := ensureTenantScope(StoreScope(uuid.New(), uuid.New())); err != nil {
		t.Errorf("ensureTenantScope(store scope) = %v, want nil", err)
	}

	cases := []Scope{{}, {StoreID: uuid.New()}}
	for _, scope := range cases {
		err := ensureTenantScope(scope)
		if !errors.Is(err, ErrMissingTenantScope) {
			t.Errorf("ensureTenantScope(%+v) = %v, want ErrMissingTenantScope", scope, err)
		}
	}
}
