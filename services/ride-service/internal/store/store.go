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
var (
	ErrNotFound = errors.New("not found")
)

// Ride is the persisted ride record. Status stores the int value of the
// ridev1.RideStatus enum; convert with ridev1.RideStatus(int) when needed.
// An empty DriverID means the ride has no assigned driver.
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
	Status         int
	Fare           float64
	DistanceMeters float64
	RequestedAt    int64 // unix seconds
	CompletedAt    int64 // unix seconds
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

// Migrate creates the rides table if it does not exist.
func (s *Store) Migrate(ctx context.Context) error {
	const q = `CREATE TABLE IF NOT EXISTS rides (
		id            TEXT PRIMARY KEY,
		passenger_id  TEXT NOT NULL,
		driver_id     TEXT NOT NULL DEFAULT '',
		pickup_lat    REAL NOT NULL,
		pickup_lng    REAL NOT NULL,
		pickup_addr   TEXT NOT NULL DEFAULT '',
		dropoff_lat   REAL NOT NULL,
		dropoff_lng   REAL NOT NULL,
		dropoff_addr  TEXT NOT NULL DEFAULT '',
		status        INTEGER NOT NULL,
		fare          REAL NOT NULL DEFAULT 0,
		distance_meters REAL NOT NULL DEFAULT 0,
		requested_at  INTEGER NOT NULL,
		completed_at  INTEGER NOT NULL DEFAULT 0
	);`
	_, err := s.db.ExecContext(ctx, q)
	return err
}

const rideColumns = `id,passenger_id,driver_id,pickup_lat,pickup_lng,pickup_addr,dropoff_lat,dropoff_lng,dropoff_addr,status,fare,distance_meters,requested_at,completed_at`

func scanRide(sc interface{ Scan(dest ...any) error }) (*Ride, error) {
	var r Ride
	err := sc.Scan(
		&r.ID, &r.PassengerID, &r.DriverID,
		&r.PickupLat, &r.PickupLng, &r.PickupAddr,
		&r.DropoffLat, &r.DropoffLng, &r.DropoffAddr,
		&r.Status, &r.Fare, &r.DistanceMeters, &r.RequestedAt, &r.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Create inserts a new ride.
func (s *Store) Create(ctx context.Context, r *Ride) error {
	const q = `INSERT INTO rides (id,passenger_id,driver_id,pickup_lat,pickup_lng,pickup_addr,dropoff_lat,dropoff_lng,dropoff_addr,status,fare,distance_meters,requested_at,completed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, q,
		r.ID, r.PassengerID, r.DriverID,
		r.PickupLat, r.PickupLng, r.PickupAddr,
		r.DropoffLat, r.DropoffLng, r.DropoffAddr,
		r.Status, r.Fare, r.DistanceMeters, r.RequestedAt, r.CompletedAt)
	return err
}

// Get fetches a ride by id.
func (s *Store) Get(ctx context.Context, id string) (*Ride, error) {
	q := `SELECT ` + rideColumns + ` FROM rides WHERE id=?`
	r, err := scanRide(s.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Update persists the mutable fields (driver_id, status, fare, completed_at).
// Returns ErrNotFound if the ride id does not exist.
func (s *Store) Update(ctx context.Context, r *Ride) error {
	const q = `UPDATE rides SET driver_id=?, status=?, fare=?, completed_at=? WHERE id=?`
	res, err := s.db.ExecContext(ctx, q, r.DriverID, r.Status, r.Fare, r.CompletedAt, r.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByUser lists rides for a passenger or driver, newest first.
// role "driver" filters by driver_id, otherwise passenger_id is used.
func (s *Store) ListByUser(ctx context.Context, userID, role string, limit int) ([]*Ride, error) {
	col := "passenger_id"
	if role == "driver" {
		col = "driver_id"
	}
	q := fmt.Sprintf(`SELECT %s FROM rides WHERE %s=? ORDER BY requested_at DESC LIMIT ?`, rideColumns, col)
	rows, err := s.db.QueryContext(ctx, q, userID, limit)
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
