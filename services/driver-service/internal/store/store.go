// Package store is the driver-service persistence layer backed by libsql.
package store

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
)

// Sentinel errors.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// Driver is the persisted driver record. The driver id is the same as the
// user id from user-service: drivers are onboarded after a user registers.
type Driver struct {
	ID            string
	LicenseNumber string
	VehiclePlate  string
	VehicleModel  string
	Rating        float64
	IsAvailable   bool
	OnboardedAt   int64 // unix seconds
}

// Store wraps the sql.DB for driver data.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New creates a driver store.
func New(db *sql.DB, log *slog.Logger) *Store {
	return &Store{db: db, log: log}
}

// Migrate creates the drivers table if it does not exist.
func (s *Store) Migrate(ctx context.Context) error {
	const q = `CREATE TABLE IF NOT EXISTS drivers (
		id             TEXT PRIMARY KEY,
		license_number  TEXT NOT NULL,
		vehicle_plate   TEXT NOT NULL,
		vehicle_model   TEXT NOT NULL,
		rating          REAL NOT NULL DEFAULT 4.8,
		is_available    INTEGER NOT NULL DEFAULT 0,
		onboarded_at    INTEGER NOT NULL
	);`
	_, err := s.db.ExecContext(ctx, q)
	return err
}

// Upsert inserts a new driver keyed by id, or updates the vehicle fields of an
// existing one. On insert rating defaults to 4.8 and is_available to false
// (enforced by the schema); onboarded_at is set to d.OnboardedAt. On conflict
// the existing rating, is_available and onboarded_at are preserved.
func (s *Store) Upsert(ctx context.Context, d *Driver) error {
	const q = `INSERT INTO drivers (id,license_number,vehicle_plate,vehicle_model,rating,is_available,onboarded_at)
		VALUES (?,?,?,?,4.8,0,?)
		ON CONFLICT(id) DO UPDATE SET
			license_number = excluded.license_number,
			vehicle_plate  = excluded.vehicle_plate,
			vehicle_model  = excluded.vehicle_model`
	_, err := s.db.ExecContext(ctx, q, d.ID, d.LicenseNumber, d.VehiclePlate, d.VehicleModel, d.OnboardedAt)
	return err
}

// GetByID fetches a driver by id.
func (s *Store) GetByID(ctx context.Context, id string) (*Driver, error) {
	const q = `SELECT id,license_number,vehicle_plate,vehicle_model,rating,is_available,onboarded_at FROM drivers WHERE id=?`
	var d Driver
	var avail int
	err := s.db.QueryRowContext(ctx, q, id).
		Scan(&d.ID, &d.LicenseNumber, &d.VehiclePlate, &d.VehicleModel, &d.Rating, &avail, &d.OnboardedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.IsAvailable = avail == 1
	return &d, nil
}

// SetAvailability sets the availability flag for a driver.
func (s *Store) SetAvailability(ctx context.Context, id string, available bool) error {
	avail := 0
	if available {
		avail = 1
	}
	const q = `UPDATE drivers SET is_available=? WHERE id=?`
	res, err := s.db.ExecContext(ctx, q, avail, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
