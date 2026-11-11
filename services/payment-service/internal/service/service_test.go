package service_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	paymentv1 "ms/proto/gen/payment/v1"
	"ms/shared/testutil"
	"ms/payment-service/internal/service"
	"ms/payment-service/internal/store"
)

func migratePayments(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newSvc(t *testing.T) *service.Server {
	t.Helper()
	db := testutil.NewMemDB(t, migratePayments)
	return service.New(store.New(db, slog.Default()), slog.Default())
}

func TestChargeRide_Success(t *testing.T) {
	svc := newSvc(t)
	resp, err := svc.ChargeRide(context.Background(), &paymentv1.ChargeRideRequest{
		RideId: "r_1", PassengerId: "p_1", DriverId: "d_1", Amount: 8.50,
	})
	if err != nil {
		t.Fatalf("ChargeRide: %v", err)
	}
	if resp.GetAmount() != 8.50 {
		t.Errorf("expected amount 8.50, got %f", resp.GetAmount())
	}
	if resp.GetStatus() != paymentv1.PaymentStatus_COMPLETED {
		t.Errorf("expected COMPLETED, got %v", resp.GetStatus())
	}
}

func TestChargeRide_MissingRideID(t *testing.T) {
	svc := newSvc(t)
	_, err := svc.ChargeRide(context.Background(), &paymentv1.ChargeRideRequest{Amount: 5.0})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestChargeRide_Idempotent(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	req := &paymentv1.ChargeRideRequest{RideId: "r_1", Amount: 10.0}

	p1, _ := svc.ChargeRide(ctx, req)
	req.Amount = 99.0 // change amount on second call
	p2, _ := svc.ChargeRide(ctx, req)

	if p1.GetId() != p2.GetId() {
		t.Errorf("expected same id on second call: %s vs %s", p1.GetId(), p2.GetId())
	}
	if p2.GetAmount() != 10.0 {
		t.Errorf("original amount should be preserved, got %f", p2.GetAmount())
	}
}

func TestGetPayment_NotFound(t *testing.T) {
	svc := newSvc(t)
	_, err := svc.GetPayment(context.Background(), &paymentv1.GetPaymentRequest{RideId: "ghost"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestListPayments(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()

	for _, req := range []*paymentv1.ChargeRideRequest{
		{RideId: "r_1", PassengerId: "p_1", DriverId: "d_1", Amount: 5.0},
		{RideId: "r_2", PassengerId: "p_1", DriverId: "d_2", Amount: 7.0},
		{RideId: "r_3", PassengerId: "p_2", DriverId: "d_1", Amount: 9.0},
	} {
		if _, err := svc.ChargeRide(ctx, req); err != nil {
			t.Fatalf("ChargeRide: %v", err)
		}
	}

	resp, err := svc.ListPayments(ctx, &paymentv1.ListPaymentsRequest{UserId: "p_1", Role: "passenger", Limit: 10})
	if err != nil {
		t.Fatalf("ListPayments: %v", err)
	}
	if len(resp.GetPayments()) != 2 {
		t.Fatalf("expected 2 payments, got %d", len(resp.GetPayments()))
	}
	if resp.GetTotal() != 12.0 {
		t.Errorf("expected total 12.0, got %f", resp.GetTotal())
	}
}
