package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"ms/location-service/internal/store"
	"ms/shared/testutil"
)

func migrateLocations(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(testutil.NewMemDB(t, migrateLocations), slog.Default())
}

func TestUpsert_and_Get(t *testing.T) {
	s := newStore(t)

	loc := &store.DriverLocation{
		DriverID: "drv_1", Latitude: 40.71, Longitude: -74.00,
		Heading: 90, Speed: 10, UpdatedAt: 1000,
	}
	if err := s.Upsert(context.Background(), loc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, ok := s.Get("drv_1")
	if !ok {
		t.Fatal("expected driver to be found in cache")
	}
	if got.Latitude != 40.71 || got.Longitude != -74.00 {
		t.Errorf("unexpected values: %+v", got)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newStore(t)
	_, ok := s.Get("ghost")
	if ok {
		t.Error("expected not found for unknown driver")
	}
}

func TestUpsert_UpdatesExisting(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ = s.Upsert(ctx, &store.DriverLocation{DriverID: "drv_1", Latitude: 10, Longitude: 20, UpdatedAt: 1})
	_ = s.Upsert(ctx, &store.DriverLocation{DriverID: "drv_1", Latitude: 11, Longitude: 21, UpdatedAt: 2})

	got, _ := s.Get("drv_1")
	if got.Latitude != 11 || got.Longitude != 21 {
		t.Errorf("expected updated position, got %+v", got)
	}
}

func TestAll_and_Count(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for i, id := range []string{"drv_1", "drv_2", "drv_3"} {
		_ = s.Upsert(ctx, &store.DriverLocation{
			DriverID: id, Latitude: float64(40 + i), Longitude: -74.0, UpdatedAt: 1000,
		})
	}
	if s.Count() != 3 {
		t.Fatalf("expected 3, got %d", s.Count())
	}
	all := s.All()
	if len(all) != 3 {
		t.Fatalf("All() expected 3, got %d", len(all))
	}
}

func TestLoadAll(t *testing.T) {
	// Pre-populate DB, create a fresh store, LoadAll, verify cache is hydrated.
	db := testutil.NewMemDB(t, migrateLocations)
	s1 := store.New(db, slog.Default())
	ctx := context.Background()

	for i, id := range []string{"drv_1", "drv_2"} {
		_ = s1.Upsert(ctx, &store.DriverLocation{
			DriverID: id, Latitude: float64(40 + i), Longitude: -74.0, UpdatedAt: 1000,
		})
	}

	// New store instance over same DB — cache is empty until LoadAll.
	s2 := store.New(db, slog.Default())
	if s2.Count() != 0 {
		t.Fatal("fresh store should have empty cache")
	}
	if err := s2.LoadAll(ctx); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if s2.Count() != 2 {
		t.Fatalf("expected 2 after LoadAll, got %d", s2.Count())
	}
}
