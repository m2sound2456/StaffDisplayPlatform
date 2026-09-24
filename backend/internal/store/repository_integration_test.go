package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m2sound2456/staffdisplay/backend/internal/database"
	"github.com/m2sound2456/staffdisplay/backend/internal/store"
	"github.com/m2sound2456/staffdisplay/backend/internal/testsupport"
)

// setupTenants migrates the test schema and creates two isolated tenants, the
// two sides of the isolation scenarios in docs/BLUEPRINT.md §22.
func setupTenants(t *testing.T) (*database.Database, uuid.UUID, uuid.UUID) {
	t.Helper()

	db := testsupport.Open(t)
	testsupport.MigrateSchema(t, db)
	return db, testsupport.NewTenant(t, db, "Tenant A"), testsupport.NewTenant(t, db, "Tenant B")
}

// mustCreate builds a validated store through the domain and persists it.
func mustCreate(t *testing.T, repository store.Repository, scope store.Scope, name, slug string) *store.Store {
	t.Helper()

	created, err := store.New(scope, store.CreateInput{Name: name, Slug: slug})
	if err != nil {
		t.Fatalf("store.New(%q, %q) error = %v", name, slug, err)
	}
	if err := repository.Create(context.Background(), scope, created); err != nil {
		t.Fatalf("create store %q: %v", slug, err)
	}
	return created
}

func TestRepositoryCreateUsesScopeAsTenantAuthority(t *testing.T) {
	db, tenantA, tenantB := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	created, err := store.New(store.TenantScope(tenantA), store.CreateInput{Name: "  Coffee A ", Slug: "Coffee A"})
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	// A hostile caller claims another tenant: the scope must win.
	created.TenantID = tenantB

	if err := repository.Create(ctx, store.TenantScope(tenantA), created); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.TenantID != tenantA {
		t.Errorf("TenantID = %s, want the scope tenant %s", created.TenantID, tenantA)
	}

	loaded, err := repository.Get(ctx, store.TenantScope(tenantA), created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.Name != "Coffee A" || loaded.Slug != "coffee-a" {
		t.Errorf("loaded store = %q/%q, want Coffee A/coffee-a", loaded.Name, loaded.Slug)
	}
	if loaded.Status != store.StatusActive {
		t.Errorf("Status = %q, want %q", loaded.Status, store.StatusActive)
	}
	if loaded.Timezone != store.DefaultTimezone {
		t.Errorf("Timezone = %q, want %q", loaded.Timezone, store.DefaultTimezone)
	}
	if string(loaded.OpeningHours) != store.DefaultOpeningHours {
		t.Errorf("OpeningHours = %s, want %s", loaded.OpeningHours, store.DefaultOpeningHours)
	}
	if loaded.CreatedAt.IsZero() || loaded.UpdatedAt.IsZero() {
		t.Error("timestamps must be persisted")
	}
}

func TestRepositoryRejectsInvalidStoresBeforeWriting(t *testing.T) {
	db, tenantA, _ := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	reserved, err := store.New(store.TenantScope(tenantA), store.CreateInput{Name: "Shop", Slug: "shop"})
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	reserved.Slug = "api" // reserved platform path, bypassing the domain helper

	if err := repository.Create(ctx, store.TenantScope(tenantA), reserved); !errors.Is(err, store.ErrValidation) {
		t.Fatalf("Create(reserved slug) = %v, want ErrValidation", err)
	}

	stores, err := repository.List(ctx, store.TenantScope(tenantA), store.ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(stores) != 0 {
		t.Fatalf("a rejected store must not be written, found %d rows", len(stores))
	}
}

func TestRepositoryCreateWithUnknownTenantReportsTenantNotFound(t *testing.T) {
	db, _, _ := setupTenants(t)
	repository := store.NewRepository(db.Gorm())

	scope := store.TenantScope(uuid.New()) // never inserted
	created, err := store.New(scope, store.CreateInput{Name: "Orphan", Slug: "orphan"})
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}

	if err := repository.Create(context.Background(), scope, created); !errors.Is(err, store.ErrTenantNotFound) {
		t.Fatalf("Create() = %v, want ErrTenantNotFound (FK violation)", err)
	}
}

func TestDatabaseRejectsReservedSlugEvenWhenTheDomainIsBypassed(t *testing.T) {
	db, tenantA, _ := setupTenants(t)

	err := db.Gorm().Exec(
		"INSERT INTO stores (tenant_id, name, slug) VALUES (?, ?, ?)",
		tenantA.String(), "Reserved", "app",
	).Error
	if err == nil {
		t.Fatal("expected the stores_slug_not_reserved constraint to reject the slug")
	}
	if !strings.Contains(err.Error(), "stores_slug_not_reserved") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositorySlugIsGloballyUniqueAndCaseInsensitive(t *testing.T) {
	db, tenantA, tenantB := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	mustCreate(t, repository, store.TenantScope(tenantA), "Coffee", "coffee")

	// Same tenant, different case: the public /s/{slug} route cannot tell them
	// apart, so the database (partial unique index) must reject it.
	duplicate, err := store.New(store.TenantScope(tenantA), store.CreateInput{Name: "Coffee 2", Slug: "COFFEE"})
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	if err := repository.Create(ctx, store.TenantScope(tenantA), duplicate); !errors.Is(err, store.ErrSlugTaken) {
		t.Fatalf("Create(duplicate slug) = %v, want ErrSlugTaken", err)
	}

	// Another tenant cannot take the slug either: the display URL is not tenant
	// qualified (single domain + path).
	foreign, err := store.New(store.TenantScope(tenantB), store.CreateInput{Name: "Coffee B", Slug: "coffee"})
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	if err := repository.Create(ctx, store.TenantScope(tenantB), foreign); !errors.Is(err, store.ErrSlugTaken) {
		t.Fatalf("Create(cross tenant duplicate slug) = %v, want ErrSlugTaken", err)
	}

	taken, err := repository.SlugTaken(ctx, " CoFFee ", nil)
	if err != nil {
		t.Fatalf("SlugTaken() error = %v", err)
	}
	if !taken {
		t.Error("SlugTaken(coffee) = false, want true")
	}

	listed, err := repository.List(ctx, store.TenantScope(tenantB), store.ListFilter{})
	if err != nil {
		t.Fatalf("List(tenant B) error = %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("tenant B must not own stores, found %d", len(listed))
	}
}

func TestRepositorySoftDeleteKeepsRowAndReleasesSlug(t *testing.T) {
	db, tenantA, _ := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	created := mustCreate(t, repository, store.TenantScope(tenantA), "Temporary", "temp-shop")

	if err := repository.Delete(ctx, store.TenantScope(tenantA), created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := repository.Get(ctx, store.TenantScope(tenantA), created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(deleted store) = %v, want ErrNotFound", err)
	}
	if _, err := repository.GetBySlug(ctx, "temp-shop"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetBySlug(deleted store) = %v, want ErrNotFound", err)
	}

	// The row survives so audit history and forensic queries stay intact.
	var persisted struct {
		DeletedAt *time.Time
	}
	if err := db.Gorm().Raw("SELECT deleted_at FROM stores WHERE id = ?", created.ID.String()).Scan(&persisted).Error; err != nil {
		t.Fatalf("read deleted row: %v", err)
	}
	if persisted.DeletedAt == nil {
		t.Fatal("soft delete must keep the row with a deleted_at timestamp")
	}

	taken, err := repository.SlugTaken(ctx, "temp-shop", nil)
	if err != nil {
		t.Fatalf("SlugTaken() error = %v", err)
	}
	if taken {
		t.Error("SlugTaken(deleted slug) = true, want false (the slug is free again)")
	}

	// Reusing the released slug publishes a brand new display route.
	replacement := mustCreate(t, repository, store.TenantScope(tenantA), "Temporary 2", "temp-shop")
	if replacement.ID == created.ID {
		t.Error("the replacement store must be a new row")
	}
	resolved, err := repository.GetBySlug(ctx, "TEMP-SHOP")
	if err != nil {
		t.Fatalf("GetBySlug(replacement) error = %v", err)
	}
	if resolved.ID != replacement.ID {
		t.Errorf("GetBySlug resolved %s, want the replacement %s", resolved.ID, replacement.ID)
	}
}

// TestRepositoryIsolatesTenantsOnRead is the executable form of BLUEPRINT §22
// "Multi-tenant isolation": tenant A only ever reads its own stores, and a
// foreign id answers ErrNotFound (the API renders that as 404 so existence
// never leaks).
func TestRepositoryIsolatesTenantsOnRead(t *testing.T) {
	db, tenantA, tenantB := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	storeA := mustCreate(t, repository, store.TenantScope(tenantA), "Shop A", "shop-a")
	storeB := mustCreate(t, repository, store.TenantScope(tenantB), "Shop B", "shop-b")

	if _, err := repository.Get(ctx, store.TenantScope(tenantA), storeA.ID); err != nil {
		t.Fatalf("tenant A must read its own store: %v", err)
	}

	if _, err := repository.Get(ctx, store.TenantScope(tenantA), storeB.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tenant A reading tenant B store = %v, want ErrNotFound", err)
	}

	listA, err := repository.List(ctx, store.TenantScope(tenantA), store.ListFilter{})
	if err != nil {
		t.Fatalf("List(tenant A) error = %v", err)
	}
	if len(listA) != 1 || listA[0].ID != storeA.ID {
		t.Fatalf("tenant A listing = %+v, want only its own store", listA)
	}

	listB, err := repository.List(ctx, store.TenantScope(tenantB), store.ListFilter{})
	if err != nil {
		t.Fatalf("List(tenant B) error = %v", err)
	}
	if len(listB) != 1 || listB[0].ID != storeB.ID {
		t.Fatalf("tenant B listing = %+v, want only its own store", listB)
	}

	// The public display route is tenant independent by design: the slug is
	// globally unique, so /s/{slug} resolves exactly one store (BLUEPRINT §4).
	resolved, err := repository.GetBySlug(ctx, "SHOP-B")
	if err != nil {
		t.Fatalf("GetBySlug(shop-b) error = %v", err)
	}
	if resolved.ID != storeB.ID {
		t.Errorf("GetBySlug resolved %s, want %s", resolved.ID, storeB.ID)
	}
}

func TestRepositoryIsolatesTenantsOnUpdate(t *testing.T) {
	db, tenantA, tenantB := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	mustCreate(t, repository, store.TenantScope(tenantA), "Shop A", "shop-a")
	storeB := mustCreate(t, repository, store.TenantScope(tenantB), "Shop B", "shop-b")

	foreign, err := repository.Get(ctx, store.TenantScope(tenantB), storeB.ID)
	if err != nil {
		t.Fatalf("Get(tenant B store) error = %v", err)
	}
	foreign.Name = "Hijacked"

	// Tenant A knows the id of tenant B's store but must not be able to write it.
	if err := repository.Update(ctx, store.TenantScope(tenantA), foreign); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross tenant Update() = %v, want ErrNotFound", err)
	}

	// A payload that claims tenant A while carrying tenant B's id is refused too.
	foreign.TenantID = tenantA
	if err := repository.Update(ctx, store.TenantScope(tenantA), foreign); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("spoofed tenant Update() = %v, want ErrNotFound", err)
	}

	unchanged, err := repository.Get(ctx, store.TenantScope(tenantB), storeB.ID)
	if err != nil {
		t.Fatalf("Get(tenant B store) error = %v", err)
	}
	if unchanged.Name != "Shop B" {
		t.Errorf("row changed by a foreign tenant: Name = %q, want Shop B", unchanged.Name)
	}
}

func TestRepositoryIsolatesTenantsOnDelete(t *testing.T) {
	db, tenantA, tenantB := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	storeA := mustCreate(t, repository, store.TenantScope(tenantA), "Shop A", "shop-a")
	storeB := mustCreate(t, repository, store.TenantScope(tenantB), "Shop B", "shop-b")

	if err := repository.Delete(ctx, store.TenantScope(tenantA), storeB.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross tenant Delete() = %v, want ErrNotFound", err)
	}
	if _, err := repository.Get(ctx, store.TenantScope(tenantB), storeB.ID); err != nil {
		t.Fatalf("tenant B store must survive a foreign delete: %v", err)
	}

	// The owner can retire its own store, and a second delete stays a 404.
	if err := repository.Delete(ctx, store.TenantScope(tenantA), storeA.ID); err != nil {
		t.Fatalf("Delete(own store) error = %v", err)
	}
	if _, err := repository.Get(ctx, store.TenantScope(tenantA), storeA.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(after delete) = %v, want ErrNotFound", err)
	}
	if err := repository.Delete(ctx, store.TenantScope(tenantA), storeA.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second Delete() = %v, want ErrNotFound", err)
	}
}

func TestRepositoryUpdateValidatesAndReportsUnknownRows(t *testing.T) {
	db, tenantA, _ := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	created := mustCreate(t, repository, store.TenantScope(tenantA), "Shop A", "shop-a")

	created.Name = "  Shop A Renamed "
	created.OpeningHours = []byte(`{"mon":"10:00-20:00"}`)
	if err := repository.Update(ctx, store.TenantScope(tenantA), created); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded, err := repository.Get(ctx, store.TenantScope(tenantA), created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if reloaded.Name != "Shop A Renamed" {
		t.Errorf("Name = %q, want Shop A Renamed", reloaded.Name)
	}
	if string(reloaded.OpeningHours) == "" {
		t.Fatal("OpeningHours must not be empty")
	}
	var openingHours map[string]string
	if err := json.Unmarshal(reloaded.OpeningHours, &openingHours); err != nil {
		t.Fatalf("OpeningHours is not a JSON object (%s): %v", reloaded.OpeningHours, err)
	}
	if openingHours["mon"] != "10:00-20:00" {
		t.Errorf("OpeningHours = %v, want mon=10:00-20:00", openingHours)
	}
	if reloaded.UpdatedAt.Before(reloaded.CreatedAt) {
		t.Errorf("UpdatedAt = %s must not precede CreatedAt = %s", reloaded.UpdatedAt, reloaded.CreatedAt)
	}

	created.Slug = "app" // reserved platform path
	if err := repository.Update(ctx, store.TenantScope(tenantA), created); !errors.Is(err, store.ErrValidation) {
		t.Fatalf("Update(reserved slug) = %v, want ErrValidation", err)
	}

	ghost := &store.Store{
		ID:           uuid.New(),
		TenantID:     tenantA,
		Name:         "Ghost",
		Slug:         "ghost",
		Status:       store.StatusActive,
		Timezone:     store.DefaultTimezone,
		OpeningHours: []byte(`{}`),
	}
	if err := repository.Update(ctx, store.TenantScope(tenantA), ghost); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update(unknown store) = %v, want ErrNotFound", err)
	}
}

func TestRepositoryListFiltersAndPages(t *testing.T) {
	db, tenantA, _ := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	mustCreate(t, repository, store.TenantScope(tenantA), "Alpha", "alpha")
	mustCreate(t, repository, store.TenantScope(tenantA), "Charlie", "charlie")

	inactive, err := store.New(store.TenantScope(tenantA), store.CreateInput{
		Name: "Bravo", Slug: "bravo", Status: store.StatusInactive,
	})
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	if err := repository.Create(ctx, store.TenantScope(tenantA), inactive); err != nil {
		t.Fatalf("Create(inactive store) error = %v", err)
	}

	all, err := repository.List(ctx, store.TenantScope(tenantA), store.ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List() returned %d stores, want 3", len(all))
	}
	if all[0].Name != "Alpha" || all[1].Name != "Bravo" || all[2].Name != "Charlie" {
		t.Errorf("List() order = %q/%q/%q, want Alpha/Bravo/Charlie", all[0].Name, all[1].Name, all[2].Name)
	}

	inactiveStatus := store.StatusInactive
	filtered, err := repository.List(ctx, store.TenantScope(tenantA), store.ListFilter{Status: &inactiveStatus})
	if err != nil {
		t.Fatalf("List(status) error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != inactive.ID {
		t.Fatalf("List(status=inactive) = %+v, want only the inactive store", filtered)
	}
	if filtered[0].Status != store.StatusInactive {
		t.Errorf("Status = %q, want %q", filtered[0].Status, store.StatusInactive)
	}

	page, err := repository.List(ctx, store.TenantScope(tenantA), store.ListFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("List(page) error = %v", err)
	}
	if len(page) != 1 || page[0].Name != "Bravo" {
		t.Fatalf("List(limit=1, offset=1) = %+v, want Bravo", page)
	}
}

func TestRepositoryReportsUnknownAndInvalidIdentifiers(t *testing.T) {
	db, tenantA, _ := setupTenants(t)
	repository := store.NewRepository(db.Gorm())
	ctx := context.Background()

	created := mustCreate(t, repository, store.TenantScope(tenantA), "Shop A", "shop-a")

	if _, err := repository.Get(ctx, store.TenantScope(tenantA), uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get(unknown id) = %v, want ErrNotFound", err)
	}
	if _, err := repository.Get(ctx, store.TenantScope(tenantA), uuid.Nil); !errors.Is(err, store.ErrValidation) {
		t.Fatalf("Get(zero id) = %v, want ErrValidation", err)
	}
	if _, err := repository.GetBySlug(ctx, "  "); !errors.Is(err, store.ErrValidation) {
		t.Fatalf("GetBySlug(blank) = %v, want ErrValidation", err)
	}
	if _, err := repository.GetBySlug(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetBySlug(unknown) = %v, want ErrNotFound", err)
	}

	// SlugTaken ignores the store itself, so a rename to its own slug is allowed.
	taken, err := repository.SlugTaken(ctx, "shop-a", &created.ID)
	if err != nil {
		t.Fatalf("SlugTaken() error = %v", err)
	}
	if taken {
		t.Error("SlugTaken(own slug, own id) = true, want false")
	}
	if _, err := repository.SlugTaken(ctx, "!!!", nil); !errors.Is(err, store.ErrValidation) {
		t.Fatalf("SlugTaken(invalid slug) = %v, want ErrValidation", err)
	}
}
