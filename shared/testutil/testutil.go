// Package testutil provides helpers shared across service test packages.
package testutil

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

// NewMemDB opens a fresh in-memory libsql database, runs the supplied
// migrate function and registers t.Cleanup to close it.
// Each call returns an independent database (unique URI via ?cache=shared and
// a unique name so parallel subtests get isolation).
func NewMemDB(t *testing.T, migrate func(context.Context, *sql.DB) error) *sql.DB {
	t.Helper()
	// Use file::memory: with a unique name per test to get per-test isolation.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := sql.Open("libsql", dsn)
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
