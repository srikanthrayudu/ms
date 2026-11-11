package service_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	driverv1 "ms/proto/gen/driver/v1"
	locationv1 "ms/proto/gen/location/v1"
	ridev1 "ms/proto/gen/ride/v1"
	"ms/shared/testutil"
	"ms/ride-service/internal/service"
	"ms/ride-service/internal/store"
)

// ── fakes ──────────────────────────────────────────────────────────────────

// fakeLocationClient returns a fixed list of nearby drivers.
type fakeLocationClient struct {
	drivers []*locationv1.NearbyDriver
}

func (f *fakeLocationClient) UpdateLocation(_ context.Context, _ *locationv1.UpdateLocationRequest, _ ...grpc.CallOption) (*locationv1.UpdateLocationResponse, error) {
	return nil, nil
}
func (f *fakeLocationClient) GetLocation(_ context.Context, _ *locationv1.GetLocationRequest, _ ...grpc.CallOption) (*locationv1.DriverLocation, error) {
	return nil, nil
}
func (f *fakeLocationClient) GetNearbyDrivers(_ context.Context, _ *locationv1.GetNearbyDriversRequest, _ ...grpc.CallOption) (*locationv1.GetNearbyDriversResponse, error) {
	return &locationv1.GetNearbyDriversResponse{Drivers: f.drivers}, nil
}

// fakeDriverClient marks all drivers as available unless their id is in denied.
type fakeDriverClient struct {
	available map[string]bool // nil = all available
}

func (f *fakeDriverClient) OnboardDriver(_ context.Context, _ *driverv1.OnboardDriverRequest, _ ...grpc.CallOption) (*driverv1.DriverProfile, error) {
	return nil, nil
}
func (f *fakeDriverClient) GetDriver(_ context.Context, _ *driverv1.GetDriverRequest, _ ...grpc.CallOption) (*driverv1.DriverProfile, error) {
	return nil, nil
}
func (f *fakeDriverClient) SetAvailability(_ context.Context, _ *driverv1.SetAvailabilityRequest, _ ...grpc.CallOption) (*driverv1.AvailabilityStatus, error) {
	return nil, nil
}
func (f *fakeDriverClient) GetAvailability(_ context.Context, req *driverv1.GetAvailabilityRequest, _ ...grpc.CallOption) (*driverv1.AvailabilityStatus, error) {
	avail := true
	if f.available != nil {
		avail = f.available[req.GetId()]
	}
	return &driverv1.AvailabilityStatus{Id: req.GetId(), IsAvailable: avail}, nil
}

// ── helpers ────────────────────────────────────────────────────────────────

func migrateRides(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newSvc(t *testing.T, loc locationv1.LocationServiceClient, drv driverv1.DriverServiceClient) *service.Server {
	t.Helper()
	db := testutil.NewMemDB(t, migrateRides)
	repo := store.New(db, slog.Default())
	return service.New(repo, loc, drv, nil, slog.Default()) // nil NATS = events are no-ops
}

func nearbyDriver(id string) *locationv1.NearbyDriver {
	return &locationv1.NearbyDriver{DriverId: id, Latitude: 40.71, Longitude: -74.00, DistanceMeters: 500}
}

func rideReq(passengerID string) *ridev1.RequestRideRequest {
	return &ridev1.RequestRideRequest{
		PassengerId: passengerID,
		Pickup:  &ridev1.GeoPoint{Latitude: 40.7128, Longitude: -74.0060},
		Dropoff: &ridev1.GeoPoint{Latitude: 40.7200, Longitude: -74.0000},
	}
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestRequestRide_Matched(t *testing.T) {
	loc := &fakeLocationClient{drivers: []*locationv1.NearbyDriver{nearbyDriver("drv_1")}}
	drv := &fakeDriverClient{available: map[string]bool{"drv_1": true}}
	svc := newSvc(t, loc, drv)

	resp, err := svc.RequestRide(context.Background(), rideReq("p_1"))
	if err != nil {
		t.Fatalf("RequestRide: %v", err)
	}
	if resp.GetStatus() != ridev1.RideStatus_MATCHED {
		t.Errorf("expected MATCHED, got %v", resp.GetStatus())
	}
	if resp.GetDriverId() != "drv_1" {
		t.Errorf("expected drv_1, got %s", resp.GetDriverId())
	}
	if resp.GetDistanceMeters() <= 0 {
		t.Error("distance should be positive")
	}
}

func TestRequestRide_NoDrivers(t *testing.T) {
	loc := &fakeLocationClient{drivers: nil} // no nearby drivers
	drv := &fakeDriverClient{}
	svc := newSvc(t, loc, drv)

	resp, err := svc.RequestRide(context.Background(), rideReq("p_1"))
	if err != nil {
		t.Fatalf("RequestRide: %v", err)
	}
	if resp.GetStatus() != ridev1.RideStatus_REQUESTED {
		t.Errorf("expected REQUESTED, got %v", resp.GetStatus())
	}
	if resp.GetDriverId() != "" {
		t.Errorf("driver should be unset, got %s", resp.GetDriverId())
	}
}

func TestRequestRide_AllDriversBusy(t *testing.T) {
	loc := &fakeLocationClient{drivers: []*locationv1.NearbyDriver{nearbyDriver("drv_1"), nearbyDriver("drv_2")}}
	drv := &fakeDriverClient{available: map[string]bool{"drv_1": false, "drv_2": false}}
	svc := newSvc(t, loc, drv)

	resp, _ := svc.RequestRide(context.Background(), rideReq("p_1"))
	if resp.GetStatus() != ridev1.RideStatus_REQUESTED {
		t.Errorf("expected REQUESTED when all busy, got %v", resp.GetStatus())
	}
}

func TestRequestRide_NilClients(t *testing.T) {
	// No location/driver clients — ride should still be created as REQUESTED.
	svc := newSvc(t, nil, nil)
	resp, err := svc.RequestRide(context.Background(), rideReq("p_1"))
	if err != nil {
		t.Fatalf("RequestRide with nil clients: %v", err)
	}
	if resp.GetStatus() != ridev1.RideStatus_REQUESTED {
		t.Errorf("expected REQUESTED, got %v", resp.GetStatus())
	}
}

func TestRequestRide_MissingPickup(t *testing.T) {
	svc := newSvc(t, nil, nil)
	_, err := svc.RequestRide(context.Background(), &ridev1.RequestRideRequest{PassengerId: "p_1"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestGetRide_NotFound(t *testing.T) {
	svc := newSvc(t, nil, nil)
	_, err := svc.GetRide(context.Background(), &ridev1.GetRideRequest{Id: "ghost"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestUpdateRideStatus_Lifecycle(t *testing.T) {
	loc := &fakeLocationClient{drivers: []*locationv1.NearbyDriver{nearbyDriver("drv_1")}}
	drv := &fakeDriverClient{available: map[string]bool{"drv_1": true}}
	svc := newSvc(t, loc, drv)
	ctx := context.Background()

	ride, _ := svc.RequestRide(ctx, rideReq("p_1"))
	id := ride.GetId()

	steps := []ridev1.RideStatus{
		ridev1.RideStatus_ON_THE_WAY,
		ridev1.RideStatus_ONGOING,
		ridev1.RideStatus_COMPLETED,
	}
	for _, st := range steps {
		resp, err := svc.UpdateRideStatus(ctx, &ridev1.UpdateRideStatusRequest{Id: id, Status: st})
		if err != nil {
			t.Fatalf("UpdateRideStatus %v: %v", st, err)
		}
		if resp.GetStatus() != st {
			t.Errorf("expected %v, got %v", st, resp.GetStatus())
		}
	}

	// After completion, fare should be calculated.
	final, _ := svc.GetRide(ctx, &ridev1.GetRideRequest{Id: id})
	if final.GetFare() <= 0 {
		t.Errorf("expected positive fare, got %f", final.GetFare())
	}
	if final.GetCompletedAt() == 0 {
		t.Error("completed_at should be set")
	}
}

func TestUpdateRideStatus_UnspecifiedRejected(t *testing.T) {
	loc := &fakeLocationClient{drivers: []*locationv1.NearbyDriver{nearbyDriver("drv_1")}}
	drv := &fakeDriverClient{available: map[string]bool{"drv_1": true}}
	svc := newSvc(t, loc, drv)
	ctx := context.Background()

	ride, _ := svc.RequestRide(ctx, rideReq("p_1"))
	_, err := svc.UpdateRideStatus(ctx, &ridev1.UpdateRideStatusRequest{Id: ride.GetId()})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for unspecified status, got %v", err)
	}
}

func TestListMyRides(t *testing.T) {
	svc := newSvc(t, nil, nil)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := svc.RequestRide(ctx, rideReq("p_1")); err != nil {
			t.Fatalf("RequestRide %d: %v", i, err)
		}
	}
	// Different passenger
	_, _ = svc.RequestRide(ctx, rideReq("p_2"))

	resp, err := svc.ListMyRides(ctx, &ridev1.ListMyRidesRequest{UserId: "p_1", Role: "passenger", Limit: 10})
	if err != nil {
		t.Fatalf("ListMyRides: %v", err)
	}
	if len(resp.GetRides()) != 3 {
		t.Fatalf("expected 3 rides, got %d", len(resp.GetRides()))
	}
}
