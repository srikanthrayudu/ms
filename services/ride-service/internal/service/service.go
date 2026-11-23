// Package service implements the gRPC RideService including pool (carpool) matching.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	driverv1   "ms/proto/gen/driver/v1"
	locationv1 "ms/proto/gen/location/v1"
	ridev1     "ms/proto/gen/ride/v1"
	"ms/shared/events"
	"ms/shared/geo"
	"ms/shared/resilience"
	"ms/ride-service/internal/store"
)

const (
	matchRadiusMeters = 3000
	matchLimit        = 5
	poolMaxRiders     = 3    // seats per pool trip
	poolJoinRadiusM   = 1500 // how close a passenger's pickup must be to join a pool
)

// Server implements ridev1.RideServiceServer.
type Server struct {
	ridev1.UnimplementedRideServiceServer
	repo           *store.Store
	locationClient locationv1.LocationServiceClient
	driverClient   driverv1.DriverServiceClient
	nc             *nats.Conn
	log            *slog.Logger
	surge          *geo.SurgeEngine
	locker         resilience.Locker
}

func New(repo *store.Store, loc locationv1.LocationServiceClient, drv driverv1.DriverServiceClient, nc *nats.Conn, log *slog.Logger) *Server {
	return &Server{
		repo:           repo,
		locationClient: loc,
		driverClient:   drv,
		nc:             nc,
		log:            log,
		surge:          geo.NewSurgeEngine(90 * time.Second),
		locker:         resilience.NewMemoryLocker(),
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "r_" + hex.EncodeToString(b)
}

// ─── RequestRide ───────────────────────────────────────────────────────────

func (s *Server) RequestRide(ctx context.Context, req *ridev1.RequestRideRequest) (*ridev1.Ride, error) {
	pickup := req.GetPickup()
	dropoff := req.GetDropoff()
	if pickup == nil || dropoff == nil {
		return nil, status.Error(codes.InvalidArgument, "pickup and dropoff are required")
	}

	distance := geo.Haversine(pickup.GetLatitude(), pickup.GetLongitude(), dropoff.GetLatitude(), dropoff.GetLongitude())
	rideType := store.RideTypeSolo
	if req.GetType() == ridev1.RideType_POOL {
		rideType = store.RideTypePool
	}

	// Record spatial demand and calculate dynamic surge pricing multiplier
	s.surge.RecordDemand(pickup.GetLatitude(), pickup.GetLongitude())
	surgeMult := s.surge.GetMultiplier(pickup.GetLatitude(), pickup.GetLongitude())

	// ── POOL: try to join an existing open pool trip ───────────────────────
	if rideType == store.RideTypePool {
		joined, ride, err := s.tryJoinPool(ctx, req, distance)
		if err != nil {
			s.log.Warn("pool join attempt failed", "err", err)
		}
		if joined && ride != nil {
			return ride, nil
		}
	}

	// ── Create a new ride (SOLO or new POOL trip) ──────────────────────────
	r := &store.Ride{
		ID:              newID(),
		PassengerID:     req.GetPassengerId(),
		PickupLat:       pickup.GetLatitude(),
		PickupLng:       pickup.GetLongitude(),
		PickupAddr:      pickup.GetAddress(),
		DropoffLat:      dropoff.GetLatitude(),
		DropoffLng:      dropoff.GetLongitude(),
		DropoffAddr:     dropoff.GetAddress(),
		Status:          int(ridev1.RideStatus_REQUESTED),
		DistanceMeters:  distance,
		RequestedAt:     time.Now().Unix(),
		RideType:        rideType,
		MaxRiders:       poolMaxRiders,
		RiderCount:      1,
		SurgeMultiplier: surgeMult,
	}
	if err := s.repo.Create(ctx, r); err != nil {
		s.log.Error("create ride", "err", err)
		return nil, status.Error(codes.Internal, "failed to create ride")
	}

	// For pool trips, also insert the first rider row.
	if rideType == store.RideTypePool {
		fareShare := geo.PoolFare(distance, 1) // alone for now; recalculated as riders join
		_ = s.repo.AddRider(ctx, &store.Rider{
			RideID:      r.ID,
			PassengerID: req.GetPassengerId(),
			PickupLat:   pickup.GetLatitude(),
			PickupLng:   pickup.GetLongitude(),
			DropoffLat:  dropoff.GetLatitude(),
			DropoffLng:  dropoff.GetLongitude(),
			FareShare:   fareShare,
			Status:      store.RiderPending,
		})
	}

	s.log.Info("ride created", "id", r.ID, "type", rideType, "passenger", r.PassengerID)

	if s.tryMatchDriver(ctx, r) {
		s.publishEvent(r, ridev1.RideStatus_MATCHED)
	} else {
		s.publishEvent(r, ridev1.RideStatus_REQUESTED)
	}
	return s.rideWithRiders(ctx, r), nil
}

// tryJoinPool attempts to find an existing open pool trip the passenger can join.
// Returns (true, Ride) on success.
func (s *Server) tryJoinPool(ctx context.Context, req *ridev1.RequestRideRequest, distanceMeters float64) (bool, *ridev1.Ride, error) {
	candidates, err := s.repo.FindOpenPoolRides(ctx,
		req.GetPickup().GetLatitude(), req.GetPickup().GetLongitude(),
		poolJoinRadiusM, 20)
	if err != nil || len(candidates) == 0 {
		return false, nil, err
	}

	// Sort by distance from this passenger's pickup to the existing trip's pickup.
	type scored struct {
		ride *store.Ride
		dist float64
	}
	var sc []scored
	for _, c := range candidates {
		if c.PassengerID == req.GetPassengerId() {
			continue // don't join your own trip
		}
		d := geo.Haversine(req.GetPickup().GetLatitude(), req.GetPickup().GetLongitude(),
			c.PickupLat, c.PickupLng)
		if d <= poolJoinRadiusM {
			sc = append(sc, scored{c, d})
		}
	}
	if len(sc) == 0 {
		return false, nil, nil
	}
	sort.Slice(sc, func(i, j int) bool { return sc[i].dist < sc[j].dist })

	r := sc[0].ride
	r.RiderCount++

	// Add this passenger as a rider.
	fareShare := geo.PoolFare(distanceMeters, r.RiderCount)
	rider := &store.Rider{
		RideID:      r.ID,
		PassengerID: req.GetPassengerId(),
		PickupLat:   req.GetPickup().GetLatitude(),
		PickupLng:   req.GetPickup().GetLongitude(),
		DropoffLat:  req.GetDropoff().GetLatitude(),
		DropoffLng:  req.GetDropoff().GetLongitude(),
		FareShare:   fareShare,
		Status:      store.RiderPending,
	}
	if err := s.repo.AddRider(ctx, rider); err != nil {
		return false, nil, err
	}

	// Recalculate fare share for all existing riders now that there's one more.
	_ = s.repo.UpdateRiderFares(ctx, r.ID, fareShare)

	if err := s.repo.Update(ctx, r); err != nil {
		return false, nil, err
	}

	s.log.Info("passenger joined pool", "ride_id", r.ID, "passenger", req.GetPassengerId(), "rider_count", r.RiderCount)
	s.publishPoolJoined(r, req.GetPassengerId())
	return true, s.rideWithRiders(ctx, r), nil
}

// ─── GetRide ──────────────────────────────────────────────────────────────

func (s *Server) GetRide(ctx context.Context, req *ridev1.GetRideRequest) (*ridev1.Ride, error) {
	r, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "ride not found")
		}
		return nil, status.Error(codes.Internal, "failed to look up ride")
	}
	return s.rideWithRiders(ctx, r), nil
}

// ─── UpdateRideStatus ─────────────────────────────────────────────────────

func (s *Server) UpdateRideStatus(ctx context.Context, req *ridev1.UpdateRideStatusRequest) (*ridev1.Ride, error) {
	r, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "ride not found")
		}
		return nil, status.Error(codes.Internal, "failed to look up ride")
	}

	st := req.GetStatus()
	if st == ridev1.RideStatus_RIDE_STATUS_UNSPECIFIED {
		// Check if this is a per-rider status update (pool only).
		if req.GetRiderPassengerId() != "" && r.RideType == store.RideTypePool {
			return s.updateRiderStatus(ctx, r, req.GetRiderPassengerId(), req.GetRiderStatus())
		}
		return nil, status.Error(codes.InvalidArgument, "status is required")
	}

	r.Status = int(st)
	if st == ridev1.RideStatus_COMPLETED {
		if r.RideType == store.RideTypePool {
			// Pool fare: total = per-rider fare × rider count (factoring surge multiplier).
			perRider := geo.PoolFareWithSurge(r.DistanceMeters, r.RiderCount, r.SurgeMultiplier)
			r.Fare = perRider * float64(r.RiderCount)
			_ = s.repo.UpdateRiderFares(ctx, r.ID, perRider)
			// Mark all pending/on-board riders as dropped.
			if riders, err := s.repo.GetRiders(ctx, r.ID); err == nil {
				for _, rider := range riders {
					if rider.Status == store.RiderPending || rider.Status == store.RiderOnBoard {
						_ = s.repo.UpdateRiderStatus(ctx, r.ID, rider.PassengerID, store.RiderDropped)
					}
				}
			}
		} else {
			r.Fare = geo.SoloFareWithSurge(r.DistanceMeters, r.SurgeMultiplier)
		}
		r.CompletedAt = time.Now().Unix()
	}

	if err := s.repo.Update(ctx, r); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "ride not found")
		}
		return nil, status.Error(codes.Internal, "failed to update ride")
	}
	s.publishEvent(r, st)
	return s.rideWithRiders(ctx, r), nil
}

// updateRiderStatus updates a specific rider's pickup/dropoff state in a pool trip.
func (s *Server) updateRiderStatus(ctx context.Context, r *store.Ride, passengerID string, rst ridev1.RiderStatus) (*ridev1.Ride, error) {
	newSt := int(rst)
	if err := s.repo.UpdateRiderStatus(ctx, r.ID, passengerID, newSt); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "rider not found in this trip")
		}
		return nil, status.Error(codes.Internal, "failed to update rider")
	}
	s.log.Info("rider status updated", "ride_id", r.ID, "passenger", passengerID, "status", newSt)
	return s.rideWithRiders(ctx, r), nil
}

// ─── ListMyRides ──────────────────────────────────────────────────────────

func (s *Server) ListMyRides(ctx context.Context, req *ridev1.ListMyRidesRequest) (*ridev1.ListMyRidesResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 20
	}
	rides, err := s.repo.ListByUser(ctx, req.GetUserId(), req.GetRole(), limit)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list rides")
	}
	out := make([]*ridev1.Ride, 0, len(rides))
	for _, r := range rides {
		out = append(out, s.rideWithRiders(ctx, r))
	}
	return &ridev1.ListMyRidesResponse{Rides: out}, nil
}

// ─── Matching ─────────────────────────────────────────────────────────────

func (s *Server) tryMatchDriver(ctx context.Context, r *store.Ride) bool {
	if s.locationClient == nil || s.driverClient == nil {
		return false
	}
	nearby, err := s.locationClient.GetNearbyDrivers(ctx, &locationv1.GetNearbyDriversRequest{
		Latitude: r.PickupLat, Longitude: r.PickupLng,
		RadiusMeters: matchRadiusMeters, Limit: matchLimit,
	})
	if err != nil {
		s.log.Warn("get nearby drivers", "err", err)
		return false
	}
	for _, d := range nearby.GetDrivers() {
		driverID := d.GetDriverId()
		// Atomic driver lock prevents race conditions during concurrent matching
		unlock, err := s.locker.TryLock(ctx, "driver:"+driverID, 10*time.Second)
		if err != nil {
			continue
		}
		avail, err := s.driverClient.GetAvailability(ctx, &driverv1.GetAvailabilityRequest{Id: driverID})
		if err != nil || !avail.GetIsAvailable() {
			unlock()
			continue
		}
		r.DriverID = driverID
		r.Status = int(ridev1.RideStatus_MATCHED)
		if err := s.repo.Update(ctx, r); err != nil {
			r.DriverID = ""
			r.Status = int(ridev1.RideStatus_REQUESTED)
			unlock()
			return false
		}
		s.log.Info("ride matched", "ride_id", r.ID, "driver", r.DriverID, "type", r.RideType, "surge", r.SurgeMultiplier)
		return true
	}
	return false
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// rideWithRiders loads the riders list for a pool trip and builds the proto.
func (s *Server) rideWithRiders(ctx context.Context, r *store.Ride) *ridev1.Ride {
	proto := toProtoRide(r)
	if r.RideType == store.RideTypePool {
		if riders, err := s.repo.GetRiders(ctx, r.ID); err == nil {
			for _, rider := range riders {
				proto.Riders = append(proto.Riders, &ridev1.Rider{
					PassengerId: rider.PassengerID,
					Pickup:      &ridev1.GeoPoint{Latitude: rider.PickupLat, Longitude: rider.PickupLng},
					Dropoff:     &ridev1.GeoPoint{Latitude: rider.DropoffLat, Longitude: rider.DropoffLng},
					FareShare:   rider.FareShare,
					Status:      ridev1.RiderStatus(rider.Status),
				})
			}
		}
	}
	return proto
}

func toProtoRide(r *store.Ride) *ridev1.Ride {
	rt := ridev1.RideType_SOLO
	if r.RideType == store.RideTypePool {
		rt = ridev1.RideType_POOL
	}
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
		Type:            rt,
		MaxRiders:       int32(r.MaxRiders),
		RiderCount:      int32(r.RiderCount),
		SurgeMultiplier: r.SurgeMultiplier,
	}
}

// GetSurgeZones returns active spatial cells currently experiencing surge pricing.
func (s *Server) GetSurgeZones(ctx context.Context, _ *ridev1.GetSurgeZonesRequest) (*ridev1.GetSurgeZonesResponse, error) {
	zones := s.surge.GetActiveZones()
	out := make([]*ridev1.SurgeZone, 0, len(zones))
	for _, z := range zones {
		out = append(out, &ridev1.SurgeZone{
			CellId:      z.CellID,
			CenterLat:   z.CenterLat,
			CenterLng:   z.CenterLng,
			Multiplier:  z.Multiplier,
			DemandCount: int32(z.DemandCount),
			SupplyCount: int32(z.SupplyCount),
		})
	}
	return &ridev1.GetSurgeZonesResponse{Zones: out}, nil
}

// ─── NATS events ──────────────────────────────────────────────────────────

func eventMeta(st ridev1.RideStatus) (evType, statusName string, ok bool) {
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

func (s *Server) publishEvent(r *store.Ride, st ridev1.RideStatus) {
	if s.nc == nil {
		return
	}
	evType, statusName, ok := eventMeta(st)
	if !ok {
		return
	}
	ev := events.RideEvent{
		RideID:    r.ID,
		Type:      evType,
		Status:    statusName,
		Timestamp: events.Now(),
		IsPool:    r.RideType == store.RideTypePool,
	}
	switch st {
	case ridev1.RideStatus_MATCHED:
		ev.DriverID = r.DriverID
	case ridev1.RideStatus_COMPLETED:
		ev.Fare = r.Fare
		ev.DriverID = r.DriverID
		ev.PassengerID = r.PassengerID
	case ridev1.RideStatus_CANCELLED:
		ev.DriverID = r.DriverID
		ev.PassengerID = r.PassengerID
	}
	data, _ := json.Marshal(ev)
	_ = s.nc.Publish(events.RideSubject(r.ID, evType), data)
}

// publishPoolJoined notifies on NATS that a new rider joined an existing pool trip.
func (s *Server) publishPoolJoined(r *store.Ride, newPassengerID string) {
	if s.nc == nil {
		return
	}
	ev := events.RideEvent{
		RideID:      r.ID,
		Type:        "pool_joined",
		Status:      "MATCHED",
		DriverID:    r.DriverID,
		PassengerID: newPassengerID,
		IsPool:      true,
		Timestamp:   events.Now(),
	}
	data, _ := json.Marshal(ev)
	_ = s.nc.Publish(events.RideSubject(r.ID, "pool_joined"), data)
}
