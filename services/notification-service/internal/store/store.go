// Package store is the notification-service persistence layer backed by libsql.
package store

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
)

// ErrNotFound is returned when a record is not found.
var ErrNotFound = errors.New("not found")

// Notification is the persisted notification record.
type Notification struct {
	ID        string
	UserID    string
	Type      int    // notificationv1.NotificationType value
	Title     string
	Body      string
	RideID    string
	Read      bool
	CreatedAt int64 // unix seconds
}

// Store wraps the sql.DB for notification data.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New creates a notification store.
func New(db *sql.DB, log *slog.Logger) *Store {
	return &Store{db: db, log: log}
}

// Migrate creates the notifications table if it does not exist.
func (s *Store) Migrate(ctx context.Context) error {
	const q = `CREATE TABLE IF NOT EXISTS notifications (
		id         TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL,
		type       INTEGER NOT NULL DEFAULT 0,
		title      TEXT NOT NULL DEFAULT '',
		body       TEXT NOT NULL DEFAULT '',
		ride_id    TEXT NOT NULL DEFAULT '',
		read       INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_notif_user ON notifications(user_id, created_at DESC);`
	_, err := s.db.ExecContext(ctx, q)
	return err
}

// Create inserts a new notification.
func (s *Store) Create(ctx context.Context, n *Notification) error {
	const q = `INSERT INTO notifications (id, user_id, type, title, body, ride_id, read, created_at)
		VALUES (?,?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, q,
		n.ID, n.UserID, n.Type, n.Title, n.Body, n.RideID, 0, n.CreatedAt)
	return err
}

// List returns notifications for a user, newest first.
func (s *Store) List(ctx context.Context, userID string, unreadOnly bool, limit int) ([]*Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, user_id, type, title, body, ride_id, read, created_at
		FROM notifications WHERE user_id=?`
	if unreadOnly {
		q += ` AND read=0`
	}
	q += ` ORDER BY created_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ns []*Notification
	for rows.Next() {
		n := &Notification{}
		var readInt int
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.RideID, &readInt, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.Read = readInt != 0
		ns = append(ns, n)
	}
	return ns, rows.Err()
}

// CountUnread returns the number of unread notifications for a user.
func (s *Store) CountUnread(ctx context.Context, userID string) (int32, error) {
	var n int32
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id=? AND read=0`, userID).Scan(&n)
	return n, err
}

// MarkRead marks notifications as read. If ids is empty, all for the user are marked.
func (s *Store) MarkRead(ctx context.Context, userID string, ids []string) (int32, error) {
	var res sql.Result
	var err error
	if len(ids) == 0 {
		res, err = s.db.ExecContext(ctx,
			`UPDATE notifications SET read=1 WHERE user_id=? AND read=0`, userID)
	} else {
		// Build parameterised IN clause
		args := make([]any, 0, len(ids)+1)
		args = append(args, userID)
		ph := ""
		for i, id := range ids {
			if i > 0 {
				ph += ","
			}
			ph += "?"
			args = append(args, id)
		}
		res, err = s.db.ExecContext(ctx,
			`UPDATE notifications SET read=1 WHERE user_id=? AND id IN (`+ph+`)`, args...)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int32(n), nil
}
