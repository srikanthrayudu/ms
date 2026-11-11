// Package service implements the gRPC LocationService.
package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	locationv1 "ms/proto/gen/location/v1"
	"ms/shared/events"
	"ms/shared/geo"
	"ms/location-service/internal/store"
)

// Server implements locationv1.LocationServiceServer.
type Server struct {
	locationv1.UnimplementedLocationServiceServer
	repo *store.Store
	nc   *nats.Conn // may be nil when NATS is unavailable
	log  *slog.Logger
}

// New creates a LocationService gRPC server.
func New(repo *store.Store, nc *nats.Conn, log *slog.Logger) *Server {
	return &Server{repo: repo, nc: nc, log: log}
}

// UpdateLocation records a driver's latest position, persists it, and
// broadcasts a DriverLocationEvent on NATS.
func (s *Server) UpdateLocation(ctx context.Context, req *locationv1.UpdateLocationRequest) (*locationv1.UpdateLocationResponse, error) {
	driverID := req.GetDriverId()
	if driverID == "" {
		return nil, status.Error(codes.InvalidArgument, "driver_id is required")
	}
	lat := req.GetLatitude()
	lng := req.GetLongitude()
	if lat < -90 || lat > 90 {
		return nil, status.Error(codes.InvalidArgument, "latitude must be in [-90, 90]")
	}
	if lng < -180 || lng > 180 {
		return nil, status.Error(codes.InvalidArgument, "longitude must be in [-180, 180]")
	}

	now := time.Now().Unix()
	loc := &store.DriverLocation{
		DriverID:  driverID,
		Latitude:  lat,
		Longitude: lng,
		Heading:   req.GetHeading(),
		Speed:     req.GetSpeed(),
		UpdatedAt: now,
	}
	if err := s.repo.Upsert(ctx, loc); err != nil {
		s.log.Error("upsert location", "err", err, "driver_id", driverID)
		return nil, status.Error(codes.Internal, "failed to update location")
	}

	// Best-effort publish on NATS; failures are logged but do not fail the RPC.
	if s.nc != nil {
		evt := events.DriverLocationEvent{
			DriverID:  driverID,
			Latitude:  lat,
			Longitude: lng,
			Heading:   req.GetHeading(),
			Speed:     req.GetSpeed(),
			Timestamp: events.Now(),
		}
		if data, err := json.Marshal(evt); err != nil {
			s.log.Error("marshal location event", "err", err, "driver_id", driverID)
		} else if err := s.nc.Publish(events.DriverLocationSubject(driverID), data); err != nil {
			s.log.Error("publish location event", "err", err, "driver_id", driverID)
		}
	}

	s.log.Debug("location updated", "driver_id", driverID, "lat", lat, "lng", lng)
	return &locationv1.UpdateLocationResponse{Acknowledged: true, UpdatedAt: now}, nil
}

// GetLocation returns a driver's latest known position from the in-memory cache.
func (s *Server) GetLocation(ctx context.Context, req *locationv1.GetLocationRequest) (*locationv1.DriverLocation, error) {
	driverID := req.GetDriverId()
	if driverID == "" {
		return nil, status.Error(codes.InvalidArgument, "driver_id is required")
	}
	loc, ok := s.repo.Get(driverID)
	if !ok {
		return nil, status.Error(codes.NotFound, "driver location not found")
	}
	return toDriverLocation(loc), nil
}

// GetNearbyDrivers returns drivers within radius_meters of a point, sorted by
// distance ascending, up to limit results.
func (s *Server) GetNearbyDrivers(ctx context.Context, req *locationv1.GetNearbyDriversRequest) (*locationv1.GetNearbyDriversResponse, error) {
	lat := req.GetLatitude()
	lng := req.GetLongitude()
	radius := req.GetRadiusMeters()
	if radius <= 0 {
		radius = 3000
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 10
	}

	type nearby struct {
		loc      store.DriverLocation
		distance float64
	}
	var matches []nearby
	for _, loc := range s.repo.All() {
		d := geo.Haversine(lat, lng, loc.Latitude, loc.Longitude)
		if d <= radius {
			matches = append(matches, nearby{loc: loc, distance: d})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].distance < matches[j].distance
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}

	drivers := make([]*locationv1.NearbyDriver, 0, len(matches))
	for _, m := range matches {
		drivers = append(drivers, &locationv1.NearbyDriver{
			DriverId:       m.loc.DriverID,
			Latitude:       m.loc.Latitude,
			Longitude:      m.loc.Longitude,
			DistanceMeters: m.distance,
			Heading:        m.loc.Heading,
			UpdatedAt:      m.loc.UpdatedAt,
		})
	}
	return &locationv1.GetNearbyDriversResponse{Drivers: drivers}, nil
}

func toDriverLocation(loc store.DriverLocation) *locationv1.DriverLocation {
	return &locationv1.DriverLocation{
		DriverId:  loc.DriverID,
		Latitude:  loc.Latitude,
		Longitude: loc.Longitude,
		Heading:   loc.Heading,
		Speed:     loc.Speed,
		UpdatedAt: loc.UpdatedAt,
	}
}
