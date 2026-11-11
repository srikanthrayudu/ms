// Package store is the payment-service persistence layer backed by libsql.
package store

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
)

// Sentinel errors.
var ErrNotFound = errors.New("not found")

// Payment is the persisted payment record.
type Payment struct {
	ID          string
	RideID      string
	PassengerID string
	DriverID    string
	Amount      float64
	Status      int   // value of paymentv1.PaymentStatus enum
	CreatedAt   int64 // unix seconds
	SettledAt   int64 // unix seconds (0 = not settled)
	Notes       string
}

// Store wraps the sql.DB for payment data.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New creates a payment store.
func New(db *sql.DB, log *slog.Logger) *Store {
	return &Store{db: db, log: log}
}

// Migrate creates the payments table if it does not exist.
func (s *Store) Migrate(ctx context.Context) error {
	const q = `CREATE TABLE IF NOT EXISTS payments (
		id           TEXT PRIMARY KEY,
		ride_id      TEXT NOT NULL UNIQUE,
		passenger_id TEXT NOT NULL DEFAULT '',
		driver_id    TEXT NOT NULL DEFAULT '',
		amount       REAL NOT NULL DEFAULT 0,
		status       INTEGER NOT NULL DEFAULT 2,
		created_at   INTEGER NOT NULL,
		settled_at   INTEGER NOT NULL DEFAULT 0,
		notes        TEXT NOT NULL DEFAULT ''
	);`
	_, err := s.db.ExecContext(ctx, q)
	return err
}

// Create inserts a new payment using INSERT OR IGNORE so duplicate ride_id
// is silently skipped (idempotent). It always returns the final record.
func (s *Store) Create(ctx context.Context, p *Payment) (*Payment, error) {
	const ins = `INSERT OR IGNORE INTO payments
		(id, ride_id, passenger_id, driver_id, amount, status, created_at, settled_at, notes)
		VALUES (?,?,?,?,?,?,?,?,?)`
	if _, err := s.db.ExecContext(ctx, ins,
		p.ID, p.RideID, p.PassengerID, p.DriverID,
		p.Amount, p.Status, p.CreatedAt, p.SettledAt, p.Notes); err != nil {
		return nil, err
	}
	// Return whatever is now stored (original or the already-existing record).
	return s.GetByRideID(ctx, p.RideID)
}

// GetByRideID fetches a payment by its ride_id.
func (s *Store) GetByRideID(ctx context.Context, rideID string) (*Payment, error) {
	const q = `SELECT id, ride_id, passenger_id, driver_id, amount, status,
		created_at, settled_at, notes FROM payments WHERE ride_id=?`
	p := &Payment{}
	err := s.db.QueryRowContext(ctx, q, rideID).Scan(
		&p.ID, &p.RideID, &p.PassengerID, &p.DriverID,
		&p.Amount, &p.Status, &p.CreatedAt, &p.SettledAt, &p.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ListByUser returns payments for a passenger or driver, newest first.
// It also returns the sum of completed payment amounts.
func (s *Store) ListByUser(ctx context.Context, userID, role string, limit int) ([]*Payment, float64, error) {
	if limit <= 0 {
		limit = 20
	}
	col := "passenger_id"
	if role == "driver" {
		col = "driver_id"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ride_id, passenger_id, driver_id, amount, status,
		 created_at, settled_at, notes FROM payments
		 WHERE `+col+`=? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var payments []*Payment
	var total float64
	for rows.Next() {
		p := &Payment{}
		if err := rows.Scan(&p.ID, &p.RideID, &p.PassengerID, &p.DriverID,
			&p.Amount, &p.Status, &p.CreatedAt, &p.SettledAt, &p.Notes); err != nil {
			return nil, 0, err
		}
		payments = append(payments, p)
		if p.Status == 2 { // COMPLETED
			total += p.Amount
		}
	}
	return payments, total, rows.Err()
}
