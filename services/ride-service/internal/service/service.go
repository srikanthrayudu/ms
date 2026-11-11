// Package service implements the gRPC RideService.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	driverv1 "ms/proto/gen/driver/v1"
	locationv1 "ms/proto/gen/location/v1"
	ridev1 "ms/proto/gen/ride/v1"
	"ms/shared/events"
	"ms/shared/geo"
	"ms/ride-service/internal/store"
)

// Nearby driver search radius (meters) and result cap used during matching.
const (
	matchRadiusMeters = 3000
	matchLimit        = 5
)

// Server implements ridev1.RideServiceServer.
type Server struct {
	ridev1.UnimplementedRideServiceServer
	repo          *store.Store
	locationClient locationv1.LocationServiceClient
	driverClient   driverv1.DriverServiceClient
	nc            *nats.Conn
	log           *slog.Logger
}

// New creates a RideService gRPC server.
func New(repo *store.Store, locationClient locationv1.LocationServiceClient, driverClient driverv1.DriverServiceClient, nc *nats.Conn, log *slog.Logger) *Server {
	return &Server{repo: repo, locationClient: locationClient, driverClient: driverClient, nc: nc, log: log}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "r_" + hex.EncodeToString(b)
}

// RequestRide creates a ride, attempts to match a nearby driver, and returns
// the ride. The ride is always returned; its status is MATCHED if a driver was
// assigned, otherwise REQUESTED.
func (s *Server) RequestRide(ctx context.Context, req *ridev1.RequestRideRequest) (*ridev1.Ride, error) {
	pickup := req.GetPickup()
	dropoff := req.GetDropoff()
	if pickup == nil || dropoff == nil {
		return nil, status.Error(codes.InvalidArgument, "pickup and dropoff are required")
	}

	distance := geo.Haversine(pickup.GetLatitude(), pickup.GetLongitude(), dropoff.GetLatitude(), dropoff.GetLongitude())
	r := &store.Ride{
		ID:             newID(),
		PassengerID:    req.GetPassengerId(),
		DriverID:        "",
		PickupLat:       pickup.GetLatitude(),
		PickupLng:       pickup.GetLongitude(),
		PickupAddr:      pickup.GetAddress(),
		DropoffLat:      dropoff.GetLatitude(),
		DropoffLng:      dropoff.GetLongitude(),
		DropoffAddr:     dropoff.GetAddress(),
		Status:          int(ridev1.RideStatus_REQUESTED),
		Fare:            0,
		DistanceMeters:  distance,
		RequestedAt:     time.Now().Unix(),
		CompletedAt:     0,
	}
	if err := s.repo.Create(ctx, r); err != nil {
		s.log.Error("create ride", "err", err)
		return nil, status.Error(codes.Internal, "failed to create ride")
	}
	s.log.Info("ride requested", "id", r.ID, "passenger_id", r.PassengerID, "distance_meters", r.DistanceMeters)

	// Attempt to match a nearby available driver (nearest first).
	if s.tryMatchDriver(ctx, r) {
		s.publishEvent(r, ridev1.RideStatus_MATCHED)
	} else {
		s.publishEvent(r, ridev1.RideStatus_REQUESTED)
	}
	return toProtoRide(r), nil
}

// tryMatchDriver queries nearby drivers and assigns the first available one.
// It mutates r in place on success and returns true; on failure r is unchanged.
func (s *Server) tryMatchDriver(ctx context.Context, r *store.Ride) bool {
	if s.locationClient == nil || s.driverClient == nil {
		return false
	}
	nearby, err := s.locationClient.GetNearbyDrivers(ctx, &locationv1.GetNearbyDriversRequest{
		Latitude:      r.PickupLat,
		Longitude:     r.PickupLng,
		RadiusMeters:  matchRadiusMeters,
		Limit:         matchLimit,
	})
	if err != nil {
		s.log.Warn("get nearby drivers", "err", err)
		return false
	}
	for _, d := range nearby.GetDrivers() {
		avail, err := s.driverClient.GetAvailability(ctx, &driverv1.GetAvailabilityRequest{Id: d.GetDriverId()})
		if err != nil {
			s.log.Warn("get driver availability", "err", err, "driver_id", d.GetDriverId())
			continue
		}
		if !avail.GetIsAvailable() {
			continue
		}
		// Assign the first available, nearest driver.
		r.DriverID = d.GetDriverId()
		r.Status = int(ridev1.RideStatus_MATCHED)
		if err := s.repo.Update(ctx, r); err != nil {
			s.log.Error("update ride on match", "err", err, "ride_id", r.ID)
			// Revert in-memory state to match the still-REQUESTED row.
			r.DriverID = ""
			r.Status = int(ridev1.RideStatus_REQUESTED)
			return false
		}
		s.log.Info("ride matched", "ride_id", r.ID, "driver_id", r.DriverID)
		return true
	}
	return false
}

// GetRide returns the current state of a ride.
func (s *Server) GetRide(ctx context.Context, req *ridev1.GetRideRequest) (*ridev1.Ride, error) {
	r, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "ride not found")
		}
		s.log.Error("get ride", "err", err)
		return nil, status.Error(codes.Internal, "failed to look up ride")
	}
	return toProtoRide(r), nil
}

// UpdateRideStatus advances a ride through its lifecycle. On COMPLETED the
// fare is finalized and completed_at is set.
func (s *Server) UpdateRideStatus(ctx context.Context, req *ridev1.UpdateRideStatusRequest) (*ridev1.Ride, error) {
	r, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "ride not found")
		}
		s.log.Error("get ride", "err", err)
		return nil, status.Error(codes.Internal, "failed to look up ride")
	}
	st := req.GetStatus()
	if st == ridev1.RideStatus_RIDE_STATUS_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "status is required")
	}
	r.Status = int(st)
	if st == ridev1.RideStatus_COMPLETED {
		r.Fare = 5.0 + 1.5*(r.DistanceMeters/1000.0)
		r.CompletedAt = time.Now().Unix()
	}
	if err := s.repo.Update(ctx, r); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "ride not found")
		}
		s.log.Error("update ride", "err", err)
		return nil, status.Error(codes.Internal, "failed to update ride")
	}
	s.publishEvent(r, st)
	return toProtoRide(r), nil
}

// ListMyRides lists rides for a passenger or driver, newest first.
func (s *Server) ListMyRides(ctx context.Context, req *ridev1.ListMyRidesRequest) (*ridev1.ListMyRidesResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 20
	}
	rides, err := s.repo.ListByUser(ctx, req.GetUserId(), req.GetRole(), limit)
	if err != nil {
		s.log.Error("list rides", "err", err)
		return nil, status.Error(codes.Internal, "failed to list rides")
	}
	out := make([]*ridev1.Ride, 0, len(rides))
	for _, r := range rides {
		out = append(out, toProtoRide(r))
	}
	return &ridev1.ListMyRidesResponse{Rides: out}, nil
}

// eventMeta maps a ride status to its NATS event type and status name.
func eventMeta(st ridev1.RideStatus) (eventType, statusName string, ok bool) {
	switch st {
	case ridev1.RideStatus_REQUESTED:
		return "requested", "REQUESTED", true
	case ridev1.RideStatus_MATCHED:
		return events.RideMatched, "MATCHED", true
	case ridev1.RideStatus_ON_THE_WAY:
		return events.RideStarted, "ON_THE_WAY", true
	case ridev1.RideStatus_ONGOING:
		return events.RideOngoing, "ONGOING", true
	case ridev1.RideStatus_COMPLETED:
		return events.RideCompleted, "COMPLETED", true
	case ridev1.RideStatus_CANCELLED:
		return events.RideCancelled, "CANCELLED", true
	default:
		return "", "", false
	}
}

// publishEvent emits a ride lifecycle event on NATS. It is a no-op when the
// NATS connection is unavailable. Matched events include DriverID and
// completed events include the Fare.
func (s *Server) publishEvent(r *store.Ride, st ridev1.RideStatus) {
	if s.nc == nil {
		return
	}
	eventType, statusName, ok := eventMeta(st)
	if !ok {
		return
	}
	ev := events.RideEvent{
		RideID:    r.ID,
		Type:      eventType,
		Status:    statusName,
		Timestamp: events.Now(),
	}
	switch st {
	case ridev1.RideStatus_MATCHED:
		ev.DriverID = r.DriverID
	case ridev1.RideStatus_COMPLETED:
		ev.Fare = r.Fare
	}
	data, err := json.Marshal(ev)
	if err != nil {
		s.log.Error("marshal ride event", "err", err)
		return
	}
	subj := events.RideSubject(r.ID, eventType)
	if err := s.nc.Publish(subj, data); err != nil {
		s.log.Error("publish ride event", "err", err, "subject", subj)
	}
}

// toProtoRide converts a stored ride to its protobuf representation.
func toProtoRide(r *store.Ride) *ridev1.Ride {
	return &ridev1.Ride{
		Id:             r.ID,
		PassengerId:    r.PassengerID,
		DriverId:       r.DriverID,
		Pickup:         &ridev1.GeoPoint{Latitude: r.PickupLat, Longitude: r.PickupLng, Address: r.PickupAddr},
		Dropoff:        &ridev1.GeoPoint{Latitude: r.DropoffLat, Longitude: r.DropoffLng, Address: r.DropoffAddr},
		Status:         ridev1.RideStatus(r.Status),
		Fare:           r.Fare,
		DistanceMeters: r.DistanceMeters,
		RequestedAt:    r.RequestedAt,
		CompletedAt:    r.CompletedAt,
	}
}
