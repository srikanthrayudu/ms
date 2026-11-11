// Package service implements the gRPC DriverService.
package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	driverv1 "ms/proto/gen/driver/v1"
	"ms/driver-service/internal/store"
)

// Server implements driverv1.DriverServiceServer.
type Server struct {
	driverv1.UnimplementedDriverServiceServer
	repo *store.Store
	log  *slog.Logger
}

// New creates a DriverService gRPC server.
func New(repo *store.Store, log *slog.Logger) *Server {
	return &Server{repo: repo, log: log}
}

// OnboardDriver registers driver-specific details for an existing user. The
// driver id equals the user_id supplied in the request. If a row already
// exists for that id the vehicle fields are updated (upsert).
func (s *Server) OnboardDriver(ctx context.Context, req *driverv1.OnboardDriverRequest) (*driverv1.DriverProfile, error) {
	id := req.GetUserId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.GetLicenseNumber() == "" || req.GetVehiclePlate() == "" || req.GetVehicleModel() == "" {
		return nil, status.Error(codes.InvalidArgument, "license_number, vehicle_plate and vehicle_model are required")
	}
	d := &store.Driver{
		ID:            id,
		LicenseNumber: req.GetLicenseNumber(),
		VehiclePlate:  req.GetVehiclePlate(),
		VehicleModel:  req.GetVehicleModel(),
		OnboardedAt:   time.Now().Unix(),
	}
	if err := s.repo.Upsert(ctx, d); err != nil {
		s.log.Error("upsert driver", "id", id, "err", err)
		return nil, status.Error(codes.Internal, "failed to onboard driver")
	}
	saved, err := s.repo.GetByID(ctx, id)
	if err != nil {
		s.log.Error("reload driver", "id", id, "err", err)
		return nil, status.Error(codes.Internal, "failed to load driver")
	}
	s.log.Info("driver onboarded", "id", id)
	return toProfile(saved), nil
}

// GetDriver returns a driver's profile.
func (s *Server) GetDriver(ctx context.Context, req *driverv1.GetDriverRequest) (*driverv1.DriverProfile, error) {
	d, err := s.repo.GetByID(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "driver not found")
		}
		return nil, status.Error(codes.Internal, "failed to look up driver")
	}
	return toProfile(d), nil
}

// SetAvailability toggles whether a driver is accepting rides.
func (s *Server) SetAvailability(ctx context.Context, req *driverv1.SetAvailabilityRequest) (*driverv1.AvailabilityStatus, error) {
	if err := s.repo.SetAvailability(ctx, req.GetId(), req.GetIsAvailable()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "driver not found")
		}
		return nil, status.Error(codes.Internal, "failed to set availability")
	}
	return &driverv1.AvailabilityStatus{Id: req.GetId(), IsAvailable: req.GetIsAvailable()}, nil
}

// GetAvailability returns a driver's current availability.
func (s *Server) GetAvailability(ctx context.Context, req *driverv1.GetAvailabilityRequest) (*driverv1.AvailabilityStatus, error) {
	d, err := s.repo.GetByID(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "driver not found")
		}
		return nil, status.Error(codes.Internal, "failed to look up driver")
	}
	return &driverv1.AvailabilityStatus{Id: d.ID, IsAvailable: d.IsAvailable}, nil
}

func toProfile(d *store.Driver) *driverv1.DriverProfile {
	return &driverv1.DriverProfile{
		Id:            d.ID,
		LicenseNumber: d.LicenseNumber,
		VehiclePlate:  d.VehiclePlate,
		VehicleModel:  d.VehicleModel,
		Rating:        d.Rating,
		IsAvailable:   d.IsAvailable,
		OnboardedAt:   d.OnboardedAt,
	}
}
