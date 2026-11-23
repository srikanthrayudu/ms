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

func TestPoolRiders(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	r := sampleRide("r_pool", "p_1")
	r.RideType = store.RideTypePool
	r.MaxRiders = 3
	r.RiderCount = 1
	if err := s.Create(ctx, r); err != nil {
		t.Fatalf("Create pool ride: %v", err)
	}

	// Add rider
	rider1 := &store.Rider{
		RideID: "r_pool", PassengerID: "p_1",
		PickupLat: 40.71, PickupLng: -74.00,
		DropoffLat: 40.72, DropoffLng: -74.01,
		FareShare: 15.0, Status: store.RiderPending,
	}
	if err := s.AddRider(ctx, rider1); err != nil {
		t.Fatalf("AddRider: %v", err)
	}

	riders, err := s.GetRiders(ctx, "r_pool")
	if err != nil {
		t.Fatalf("GetRiders: %v", err)
	}
	if len(riders) != 1 || riders[0].PassengerID != "p_1" {
		t.Fatalf("expected 1 rider with p_1, got %+v", riders)
	}

	// Update rider status
	if err := s.UpdateRiderStatus(ctx, "r_pool", "p_1", store.RiderOnBoard); err != nil {
		t.Fatalf("UpdateRiderStatus: %v", err)
	}

	// Find open pool rides
	open, err := s.FindOpenPoolRides(ctx, 40.71, -74.00, 2000, 5)
	if err != nil {
		t.Fatalf("FindOpenPoolRides: %v", err)
	}
	if len(open) != 1 || open[0].ID != "r_pool" {
		t.Fatalf("expected 1 open pool ride, got %+v", open)
	}
}
