// Package store is the user-service persistence layer backed by libsql.
package store

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
)

// Sentinel errors.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// User is the persisted user record.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	Name         string
	Phone        string
	Role         string // "passenger" | "driver"
	CreatedAt     int64  // unix seconds
}

// Store wraps the sql.DB for user data.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New creates a user store.
func New(db *sql.DB, log *slog.Logger) *Store {
	return &Store{db: db, log: log}
}

// Migrate creates the users table if it does not exist.
func (s *Store) Migrate(ctx context.Context) error {
	const q = `CREATE TABLE IF NOT EXISTS users (
		id            TEXT PRIMARY KEY,
		email          TEXT UNIQUE NOT NULL,
		password_hash  TEXT NOT NULL,
		name           TEXT NOT NULL,
		phone          TEXT NOT NULL DEFAULT '',
		role           TEXT NOT NULL,
		created_at     INTEGER NOT NULL
	);`
	_, err := s.db.ExecContext(ctx, q)
	return err
}

// Create inserts a new user.
func (s *Store) Create(ctx context.Context, u *User) error {
	const q = `INSERT INTO users (id,email,password_hash,name,phone,role,created_at)
		VALUES (?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, q, u.ID, u.Email, u.PasswordHash, u.Name, u.Phone, u.Role, u.CreatedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrAlreadyExists
	}
	return err
}

// GetByID fetches a user by id.
func (s *Store) GetByID(ctx context.Context, id string) (*User, error) {
	return s.get(ctx, `SELECT id,email,password_hash,name,phone,role,created_at FROM users WHERE id=?`, id)
}

// GetByEmail fetches a user by email.
func (s *Store) GetByEmail(ctx context.Context, email string) (*User, error) {
	return s.get(ctx, `SELECT id,email,password_hash,name,phone,role,created_at FROM users WHERE email=?`, email)
}

func (s *Store) get(ctx context.Context, q string, arg string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, q, arg).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Phone, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// UpdateProfile updates mutable profile fields.
func (s *Store) UpdateProfile(ctx context.Context, id, name, phone string) error {
	const q = `UPDATE users SET name=?, phone=? WHERE id=?`
	res, err := s.db.ExecContext(ctx, q, name, phone, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint")
}
