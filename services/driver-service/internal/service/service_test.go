package service_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	driverv1 "ms/proto/gen/driver/v1"
	"ms/shared/testutil"
	"ms/driver-service/internal/service"
	"ms/driver-service/internal/store"
)

func migrateDrivers(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newSvc(t *testing.T) *service.Server {
	t.Helper()
	db := testutil.NewMemDB(t, migrateDrivers)
	return service.New(store.New(db, slog.Default()), slog.Default())
}

func TestOnboardDriver_Success(t *testing.T) {
	svc := newSvc(t)
	resp, err := svc.OnboardDriver(context.Background(), &driverv1.OnboardDriverRequest{
		UserId: "u_1", LicenseNumber: "DL1", VehiclePlate: "NY001", VehicleModel: "Sedan",
	})
	if err != nil {
		t.Fatalf("OnboardDriver: %v", err)
	}
	if resp.GetId() != "u_1" {
		t.Errorf("unexpected id: %s", resp.GetId())
	}
	if resp.GetIsAvailable() {
		t.Error("new driver should be offline")
	}
	if resp.GetRating() != 4.8 {
		t.Errorf("expected default rating 4.8, got %f", resp.GetRating())
	}
}

func TestOnboardDriver_MissingFields(t *testing.T) {
	svc := newSvc(t)
	_, err := svc.OnboardDriver(context.Background(), &driverv1.OnboardDriverRequest{UserId: "u_1"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestSetAvailability_NotOnboarded(t *testing.T) {
	svc := newSvc(t)
	_, err := svc.SetAvailability(context.Background(), &driverv1.SetAvailabilityRequest{
		Id: "ghost", IsAvailable: true,
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestGetAvailability(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	_, _ = svc.OnboardDriver(ctx, &driverv1.OnboardDriverRequest{
		UserId: "u_1", LicenseNumber: "X", VehiclePlate: "Y", VehicleModel: "Z",
	})
	_, _ = svc.SetAvailability(ctx, &driverv1.SetAvailabilityRequest{Id: "u_1", IsAvailable: true})

	avail, err := svc.GetAvailability(ctx, &driverv1.GetAvailabilityRequest{Id: "u_1"})
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	if !avail.GetIsAvailable() {
		t.Error("expected driver to be available")
	}
}

func TestGetDriver_NotFound(t *testing.T) {
	svc := newSvc(t)
	_, err := svc.GetDriver(context.Background(), &driverv1.GetDriverRequest{Id: "ghost"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}
