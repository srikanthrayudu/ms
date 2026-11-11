package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"ms/ride-service/internal/store"
	"ms/shared/testutil"
)

func migrateRides(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(testutil.NewMemDB(t, migrateRides), slog.Default())
}

func sampleRide(id, passengerID string) *store.Ride {
	return &store.Ride{
		ID: id, PassengerID: passengerID, DriverID: "",
		PickupLat: 40.71, PickupLng: -74.00, PickupAddr: "A",
		DropoffLat: 40.72, DropoffLng: -74.01, DropoffAddr: "B",
		Status: 1, Fare: 0, DistanceMeters: 1200, RequestedAt: 1000,
	}
}

func TestCreate_and_Get(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	r := sampleRide("r_1", "p_1")
	if err := s.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, "r_1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PassengerID != "p_1" || got.DistanceMeters != 1200 {
		t.Errorf("unexpected values: %+v", got)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.Get(context.Background(), "ghost"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdate_StatusAndFare(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	r := sampleRide("r_1", "p_1")
	_ = s.Create(ctx, r)

	r.Status = 5 // COMPLETED
	r.Fare = 6.80
	r.CompletedAt = 2000
	if err := s.Update(ctx, r); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := s.Get(ctx, "r_1")
	if got.Status != 5 || got.Fare != 6.80 || got.CompletedAt != 2000 {
		t.Errorf("unexpected after update: %+v", got)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	s := newStore(t)
	r := sampleRide("ghost", "p_1")
	if err := s.Update(context.Background(), r); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListByUser_Passenger(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for i, id := range []string{"r_1", "r_2", "r_3"} {
		r := sampleRide(id, "p_1")
		r.RequestedAt = int64(i + 1)
		_ = s.Create(ctx, r)
	}
	// Different passenger — should not appear
	other := sampleRide("r_4", "p_2")
	_ = s.Create(ctx, other)

	rides, err := s.ListByUser(ctx, "p_1", "passenger", 10)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(rides) != 3 {
		t.Fatalf("expected 3 rides, got %d", len(rides))
	}
}

func TestListByUser_Driver(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	r := sampleRide("r_1", "p_1")
	r.DriverID = "drv_1"
	_ = s.Create(ctx, r)
	r2 := sampleRide("r_2", "p_2")
	r2.DriverID = "drv_2"
	_ = s.Create(ctx, r2)

	rides, err := s.ListByUser(ctx, "drv_1", "driver", 10)
	if err != nil {
		t.Fatalf("ListByUser driver: %v", err)
	}
	if len(rides) != 1 || rides[0].ID != "r_1" {
		t.Errorf("unexpected rides: %+v", rides)
	}
}
