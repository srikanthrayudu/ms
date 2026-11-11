package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"ms/payment-service/internal/store"
	"ms/shared/testutil"
)

func migratePayments(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(testutil.NewMemDB(t, migratePayments), slog.Default())
}

func samplePayment(id, rideID, passengerID, driverID string, amount float64) *store.Payment {
	return &store.Payment{
		ID: id, RideID: rideID, PassengerID: passengerID, DriverID: driverID,
		Amount: amount, Status: 2, CreatedAt: 1000, SettledAt: 1000, Notes: "test",
	}
}

func TestCreate_Idempotent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	p := samplePayment("pay_1", "ride_1", "pass_1", "drv_1", 10.0)
	got1, err := s.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Second create with different id but same ride_id — should return existing.
	p2 := samplePayment("pay_2", "ride_1", "pass_1", "drv_1", 99.0)
	got2, err := s.Create(ctx, p2)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}

	if got1.ID != got2.ID {
		t.Errorf("expected idempotent result: got1.ID=%s got2.ID=%s", got1.ID, got2.ID)
	}
	if got2.Amount != 10.0 {
		t.Errorf("original amount should be preserved, got %f", got2.Amount)
	}
}

func TestGetByRideID_NotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.GetByRideID(context.Background(), "ghost"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListByUser(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	payments := []*store.Payment{
		samplePayment("pay_1", "ride_1", "pass_1", "drv_1", 5.0),
		samplePayment("pay_2", "ride_2", "pass_1", "drv_2", 7.5),
		samplePayment("pay_3", "ride_3", "pass_2", "drv_1", 9.0),
	}
	for _, p := range payments {
		if _, err := s.Create(ctx, p); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	got, total, err := s.ListByUser(ctx, "pass_1", "passenger", 10)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 payments for pass_1, got %d", len(got))
	}
	const wantTotal = 12.5
	if total != wantTotal {
		t.Errorf("expected total %.2f, got %.2f", wantTotal, total)
	}
}

func TestListByUser_Driver(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ , _ = s.Create(ctx, samplePayment("pay_1", "ride_1", "p_1", "drv_1", 8.0))
	_ , _ = s.Create(ctx, samplePayment("pay_2", "ride_2", "p_2", "drv_2", 6.0))

	got, total, err := s.ListByUser(ctx, "drv_1", "driver", 10)
	if err != nil {
		t.Fatalf("ListByUser driver: %v", err)
	}
	if len(got) != 1 || got[0].Amount != 8.0 {
		t.Errorf("unexpected result: %+v", got)
	}
	if total != 8.0 {
		t.Errorf("expected total 8.0, got %f", total)
	}
}
