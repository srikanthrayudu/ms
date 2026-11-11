package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"ms/driver-service/internal/store"
	"ms/shared/testutil"
)

func migrateDrivers(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(testutil.NewMemDB(t, migrateDrivers), slog.Default())
}

func TestUpsert_and_GetByID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	d := &store.Driver{
		ID: "drv_1", LicenseNumber: "DL1", VehiclePlate: "NY001",
		VehicleModel: "Sedan", OnboardedAt: 1000,
	}
	if err := s.Upsert(ctx, d); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.GetByID(ctx, "drv_1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.LicenseNumber != "DL1" || got.Rating != 4.8 {
		t.Errorf("unexpected fields: %+v", got)
	}
	if got.IsAvailable {
		t.Error("new driver should be offline")
	}
}

func TestUpsert_UpdatesVehicle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	d := &store.Driver{ID: "drv_1", LicenseNumber: "DL1", VehiclePlate: "OLD", VehicleModel: "Old", OnboardedAt: 1}
	_ = s.Upsert(ctx, d)
	d.VehiclePlate = "NEW"
	_ = s.Upsert(ctx, d)

	got, _ := s.GetByID(ctx, "drv_1")
	if got.VehiclePlate != "NEW" {
		t.Errorf("plate not updated: %s", got.VehiclePlate)
	}
}

func TestSetAvailability(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ = s.Upsert(ctx, &store.Driver{ID: "drv_1", LicenseNumber: "X", VehiclePlate: "Y", VehicleModel: "Z", OnboardedAt: 1})
	if err := s.SetAvailability(ctx, "drv_1", true); err != nil {
		t.Fatalf("SetAvailability: %v", err)
	}
	got, _ := s.GetByID(ctx, "drv_1")
	if !got.IsAvailable {
		t.Error("expected driver to be available")
	}
}

func TestSetAvailability_NotFound(t *testing.T) {
	s := newStore(t)
	if err := s.SetAvailability(context.Background(), "ghost", true); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.GetByID(context.Background(), "ghost"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
