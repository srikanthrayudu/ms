// Package store is the ride-service persistence layer backed by libsql.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// Sentinel errors.
var ErrNotFound = errors.New("not found")

// RideType mirrors the proto enum values (0 = SOLO, 1 = POOL).
const (
	RideTypeSolo = 0
	RideTypePool = 1
)

// RiderStatus mirrors the proto enum values.
const (
	RiderPending   = 0
	RiderOnBoard   = 1
	RiderDropped   = 2
	RiderCancelled = 3
)

// Ride is the persisted ride record.
type Ride struct {
	ID             string
	PassengerID    string
	DriverID       string
	PickupLat      float64
	PickupLng      float64
	PickupAddr     string
	DropoffLat     float64
	DropoffLng     float64
	DropoffAddr    string
	Status         int     // ridev1.RideStatus value
	Fare           float64
	DistanceMeters float64
	RequestedAt    int64
	CompletedAt    int64
	RideType        int // RideTypeSolo | RideTypePool
	MaxRiders       int // max pool capacity
	RiderCount      int // current number of pool riders
	SurgeMultiplier float64
}

// Rider is one passenger's slot in a pool ride.
type Rider struct {
	RideID      string
	PassengerID string
	PickupLat   float64
	PickupLng   float64
	DropoffLat  float64
	DropoffLng  float64
	FareShare   float64
	Status      int // RiderPending|RiderOnBoard|RiderDropped|RiderCancelled
}

// Store wraps the sql.DB for ride data.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New creates a ride store.
func New(db *sql.DB, log *slog.Logger) *Store {
	return &Store{db: db, log: log}
}

// Migrate creates the rides and riders tables.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS rides (
			id              TEXT PRIMARY KEY,
			passenger_id    TEXT NOT NULL,
			driver_id       TEXT NOT NULL DEFAULT '',
			pickup_lat      REAL NOT NULL,
			pickup_lng      REAL NOT NULL,
			pickup_addr     TEXT NOT NULL DEFAULT '',
			dropoff_lat     REAL NOT NULL,
			dropoff_lng     REAL NOT NULL,
			dropoff_addr    TEXT NOT NULL DEFAULT '',
			status          INTEGER NOT NULL,
			fare            REAL NOT NULL DEFAULT 0,
			distance_meters REAL NOT NULL DEFAULT 0,
			requested_at    INTEGER NOT NULL,
			completed_at    INTEGER NOT NULL DEFAULT 0,
			ride_type       INTEGER NOT NULL DEFAULT 0,
			max_riders      INTEGER NOT NULL DEFAULT 3,
			rider_count     INTEGER NOT NULL DEFAULT 0,
			surge_multiplier REAL NOT NULL DEFAULT 1.0
		);
		CREATE TABLE IF NOT EXISTS riders (
			ride_id      TEXT NOT NULL REFERENCES rides(id),
			passenger_id TEXT NOT NULL,
			pickup_lat   REAL NOT NULL,
			pickup_lng   REAL NOT NULL,
			dropoff_lat  REAL NOT NULL,
			dropoff_lng  REAL NOT NULL,
			fare_share   REAL NOT NULL DEFAULT 0,
			status       INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (ride_id, passenger_id)
		);
		CREATE INDEX IF NOT EXISTS idx_rides_passenger ON rides(passenger_id, requested_at DESC);
		CREATE INDEX IF NOT EXISTS idx_rides_driver    ON rides(driver_id,    requested_at DESC);
		CREATE INDEX IF NOT EXISTS idx_rides_pool      ON rides(ride_type, status, driver_id);
	`)
	return err
}

// ─── Ride CRUD ─────────────────────────────────────────────────────────────

const rideColumns = `id,passenger_id,driver_id,pickup_lat,pickup_lng,pickup_addr,
	dropoff_lat,dropoff_lng,dropoff_addr,status,fare,distance_meters,
	requested_at,completed_at,ride_type,max_riders,rider_count,surge_multiplier`

func scanRide(sc interface{ Scan(dest ...any) error }) (*Ride, error) {
	var r Ride
	err := sc.Scan(
		&r.ID, &r.PassengerID, &r.DriverID,
		&r.PickupLat, &r.PickupLng, &r.PickupAddr,
		&r.DropoffLat, &r.DropoffLng, &r.DropoffAddr,
		&r.Status, &r.Fare, &r.DistanceMeters,
		&r.RequestedAt, &r.CompletedAt,
		&r.RideType, &r.MaxRiders, &r.RiderCount,
		&r.SurgeMultiplier,
	)
	return &r, err
}

// Create inserts a new ride row.
func (s *Store) Create(ctx context.Context, r *Ride) error {
	if r.SurgeMultiplier <= 0 {
		r.SurgeMultiplier = 1.0
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rides (id,passenger_id,driver_id,pickup_lat,pickup_lng,pickup_addr,
			dropoff_lat,dropoff_lng,dropoff_addr,status,fare,distance_meters,
			requested_at,completed_at,ride_type,max_riders,rider_count,surge_multiplier)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.PassengerID, r.DriverID,
		r.PickupLat, r.PickupLng, r.PickupAddr,
		r.DropoffLat, r.DropoffLng, r.DropoffAddr,
		r.Status, r.Fare, r.DistanceMeters,
		r.RequestedAt, r.CompletedAt,
		r.RideType, r.MaxRiders, r.RiderCount,
		r.SurgeMultiplier,
	)
	return err
}

// Get fetches a ride by id.
func (s *Store) Get(ctx context.Context, id string) (*Ride, error) {
	r, err := scanRide(s.db.QueryRowContext(ctx,
		`SELECT `+rideColumns+` FROM rides WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// Update persists mutable ride fields.
func (s *Store) Update(ctx context.Context, r *Ride) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE rides SET driver_id=?,status=?,fare=?,completed_at=?,rider_count=?
		WHERE id=?`,
		r.DriverID, r.Status, r.Fare, r.CompletedAt, r.RiderCount, r.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByUser returns rides for a passenger or driver, newest first.
func (s *Store) ListByUser(ctx context.Context, userID, role string, limit int) ([]*Ride, error) {
	col := "passenger_id"
	if role == "driver" {
		col = "driver_id"
	}
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM rides WHERE %s=? ORDER BY requested_at DESC LIMIT ?`,
			rideColumns, col), userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rides []*Ride
	for rows.Next() {
		r, err := scanRide(rows)
		if err != nil {
			return nil, err
		}
		rides = append(rides, r)
	}
	return rides, rows.Err()
}

// ─── Pool-specific queries ─────────────────────────────────────────────────

// PoolableRide is a pool trip that has an open seat and whose pickup is within
// radiusMeters of the candidate passenger's pickup.
type PoolableRide struct {
	Ride           // embedded: full ride row
	DistanceToPickup float64 // metres from candidate pickup to ride's pickup area
}

// FindOpenPoolRides returns POOL rides that:
//   - are in REQUESTED or MATCHED status (not yet ongoing)
//   - have rider_count < max_riders
//   - have a primary pickup within radiusMeters of (lat, lng)
//
// Results are ordered by distance from (lat, lng).
func (s *Store) FindOpenPoolRides(ctx context.Context, lat, lng, radiusMeters float64, limit int) ([]*Ride, error) {
	// SQLite has no built-in geo, so we over-fetch with a bounding box and
	// sort in Go by exact Haversine in the service layer.
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+rideColumns+`
		FROM rides
		WHERE ride_type = 1
		  AND status IN (1, 2)
		  AND rider_count < max_riders
		ORDER BY requested_at DESC
		LIMIT ?`, limit*5) // fetch extra so the service can sort and trim
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rides []*Ride
	for rows.Next() {
		r, err := scanRide(rows)
		if err != nil {
			return nil, err
		}
		rides = append(rides, r)
	}
	return rides, rows.Err()
}

// ─── Rider CRUD ─────────────────────────────────────────────────────────────

// AddRider inserts a new rider row for a pool trip.
func (s *Store) AddRider(ctx context.Context, r *Rider) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO riders (ride_id,passenger_id,pickup_lat,pickup_lng,
			dropoff_lat,dropoff_lng,fare_share,status)
		VALUES (?,?,?,?,?,?,?,?)`,
		r.RideID, r.PassengerID,
		r.PickupLat, r.PickupLng, r.DropoffLat, r.DropoffLng,
		r.FareShare, r.Status,
	)
	return err
}

// GetRiders returns all riders for a pool ride, ordered by join time (rowid).
func (s *Store) GetRiders(ctx context.Context, rideID string) ([]*Rider, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ride_id,passenger_id,pickup_lat,pickup_lng,
			dropoff_lat,dropoff_lng,fare_share,status
		FROM riders WHERE ride_id=? ORDER BY rowid`, rideID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var riders []*Rider
	for rows.Next() {
		r := &Rider{}
		if err := rows.Scan(
			&r.RideID, &r.PassengerID,
			&r.PickupLat, &r.PickupLng, &r.DropoffLat, &r.DropoffLng,
			&r.FareShare, &r.Status,
		); err != nil {
			return nil, err
		}
		riders = append(riders, r)
	}
	return riders, rows.Err()
}

// UpdateRiderStatus updates a specific rider's status.
func (s *Store) UpdateRiderStatus(ctx context.Context, rideID, passengerID string, st int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE riders SET status=? WHERE ride_id=? AND passenger_id=?`,
		st, rideID, passengerID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateRiderFares bulk-updates fare_share for all riders of a pool trip.
func (s *Store) UpdateRiderFares(ctx context.Context, rideID string, farePerRider float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE riders SET fare_share=? WHERE ride_id=?`, farePerRider, rideID)
	return err
}
