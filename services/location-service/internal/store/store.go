// Package store is the location-service persistence layer backed by libsql.
// It keeps an in-memory cache of the latest driver positions for fast nearby
// queries and mirrors every write to the driver_locations table for durability.
package store

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
)

// Sentinel errors.
var (
	ErrNotFound = errors.New("not found")
)

// DriverLocation is the persisted driver position record.
type DriverLocation struct {
	DriverID  string
	Latitude  float64
	Longitude float64
	Heading   float64
	Speed     float64
	UpdatedAt int64 // unix seconds
}

// Store wraps the sql.DB and an in-memory cache for fast nearby queries.
type Store struct {
	db  *sql.DB
	log *slog.Logger

	mu      sync.RWMutex
	drivers map[string]DriverLocation
}

// New creates a location store.
func New(db *sql.DB, log *slog.Logger) *Store {
	return &Store{
		db:      db,
		log:     log,
		drivers: make(map[string]DriverLocation),
	}
}

// Migrate creates the driver_locations table if it does not exist.
func (s *Store) Migrate(ctx context.Context) error {
	const q = `CREATE TABLE IF NOT EXISTS driver_locations (
		driver_id  TEXT PRIMARY KEY,
		latitude   REAL NOT NULL,
		longitude  REAL NOT NULL,
		heading    REAL NOT NULL DEFAULT 0,
		speed      REAL NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL
	);`
	_, err := s.db.ExecContext(ctx, q)
	return err
}

// LoadAll hydrates the in-memory cache from the database. Call after Migrate.
func (s *Store) LoadAll(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT driver_id, latitude, longitude, heading, speed, updated_at FROM driver_locations`)
	if err != nil {
		return err
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()
	for rows.Next() {
		var loc DriverLocation
		if err := rows.Scan(&loc.DriverID, &loc.Latitude, &loc.Longitude, &loc.Heading, &loc.Speed, &loc.UpdatedAt); err != nil {
			return err
		}
		s.drivers[loc.DriverID] = loc
	}
	return rows.Err()
}

// Upsert persists a driver location (insert or update on conflict) and
// refreshes the in-memory cache.
func (s *Store) Upsert(ctx context.Context, loc *DriverLocation) error {
	const q = `INSERT INTO driver_locations (driver_id, latitude, longitude, heading, speed, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(driver_id) DO UPDATE SET
			latitude   = excluded.latitude,
			longitude  = excluded.longitude,
			heading    = excluded.heading,
			speed      = excluded.speed,
			updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, q,
		loc.DriverID, loc.Latitude, loc.Longitude, loc.Heading, loc.Speed, loc.UpdatedAt); err != nil {
		return err
	}
	s.mu.Lock()
	s.drivers[loc.DriverID] = *loc
	s.mu.Unlock()
	return nil
}

// Get returns a driver's cached location.
func (s *Store) Get(driverID string) (DriverLocation, bool) {
	s.mu.RLock()
	loc, ok := s.drivers[driverID]
	s.mu.RUnlock()
	return loc, ok
}

// All returns a snapshot of all cached driver locations.
func (s *Store) All() []DriverLocation {
	s.mu.RLock()
	snapshot := make([]DriverLocation, 0, len(s.drivers))
	for _, loc := range s.drivers {
		snapshot = append(snapshot, loc)
	}
	s.mu.RUnlock()
	return snapshot
}

// Count returns the number of cached driver locations.
func (s *Store) Count() int {
	s.mu.RLock()
	n := len(s.drivers)
	s.mu.RUnlock()
	return n
}
