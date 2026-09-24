package store

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Isolation key columns. Keeping them in one place means a rename is a single
// edit, and the scope helpers below stay readable.
const (
	// TenantColumn carries the tenant_id isolation key.
	TenantColumn = "tenant_id"
	// StoreColumn carries the store_id isolation key of child resources.
	StoreColumn = "store_id"
	// DeletedColumn carries the soft delete marker (stores, employees, devices).
	DeletedColumn = "deleted_at"
)

// Scope is the isolation boundary of every business query: the tenant that owns
// the caller and, for store-scoped child resources (employees, devices,
// slides), the store below it.
//
// The scope is always derived from the authenticated identity (store admin
// token or device token) — never from a client supplied tenant_id/store_id on
// its own (BLUEPRINT §3.2, docs/AI_RULES.md §5.1).
type Scope struct {
	TenantID uuid.UUID
	StoreID  uuid.UUID
}

// TenantScope builds a tenant-wide scope (store management, tenant settings).
func TenantScope(tenantID uuid.UUID) Scope {
	return Scope{TenantID: tenantID}
}

// StoreScope builds a store-scoped scope (employees, devices, playlist).
func StoreScope(tenantID, storeID uuid.UUID) Scope {
	return Scope{TenantID: tenantID, StoreID: storeID}
}

// HasTenant reports whether a tenant is part of the scope.
func (s Scope) HasTenant() bool { return s.TenantID != uuid.Nil }

// HasStore reports whether a store is part of the scope.
func (s Scope) HasStore() bool { return s.StoreID != uuid.Nil }

// IsZero reports whether the scope carries no identity at all.
func (s Scope) IsZero() bool { return !s.HasTenant() && !s.HasStore() }

// String renders the scope for logs; ids are not secrets but they are the only
// identity information included.
func (s Scope) String() string {
	if s.IsZero() {
		return "scope(empty)"
	}
	if s.HasStore() {
		return fmt.Sprintf("scope(tenant=%s store=%s)", s.TenantID, s.StoreID)
	}
	return fmt.Sprintf("scope(tenant=%s)", s.TenantID)
}

// Apply narrows db to the scope. Every condition comes from the scope, so a
// caller can never widen a query by supplying an id of its own.
//
// A zero scope adds nothing — the repository *always* calls ensureTenantScope
// first, so an unscoped business query cannot be reached by accident.
func (s Scope) Apply(db *gorm.DB) *gorm.DB {
	if db == nil {
		return nil
	}
	if s.HasTenant() {
		db = db.Where(TenantColumn+" = ?", s.TenantID)
	}
	if s.HasStore() {
		db = db.Where(StoreColumn+" = ?", s.StoreID)
	}
	return db
}

// ScopedToTenant is the standalone form of Apply for callers that only have a
// tenant id (repositories of tenant-wide resources).
func ScopedToTenant(db *gorm.DB, tenantID uuid.UUID) *gorm.DB {
	return Scope{TenantID: tenantID}.Apply(db)
}

// ScopedToStore is the standalone form of Apply for child resources: it always
// applies the tenant condition too, so a store scope can never widen access.
func ScopedToStore(db *gorm.DB, tenantID, storeID uuid.UUID) *gorm.DB {
	return Scope{TenantID: tenantID, StoreID: storeID}.Apply(db)
}

// OnlyLive filters soft deleted rows for tables that do not use gorm.DeletedAt.
func OnlyLive(db *gorm.DB) *gorm.DB {
	if db == nil {
		return nil
	}
	return db.Where(DeletedColumn + " IS NULL")
}

// ensureTenantScope guards the tenant isolation invariant. Failing loudly here
// is the difference between "no data returned" and "another store's data
// returned", so it is deliberately not optional.
func ensureTenantScope(scope Scope) error {
	if scope.HasTenant() {
		return nil
	}
	return fmt.Errorf("%w: build the scope with store.TenantScope(...) or store.StoreScope(...)", ErrMissingTenantScope)
}
