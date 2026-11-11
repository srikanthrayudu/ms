package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"ms/user-service/internal/store"
	"ms/shared/testutil"
)

func migrateUsers(_ context.Context, db *sql.DB) error {
	s := store.New(db, slog.Default())
	return s.Migrate(context.Background())
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	db := testutil.NewMemDB(t, migrateUsers)
	return store.New(db, slog.Default())
}

func TestCreate_and_GetByEmail(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	u := &store.User{
		ID: "u_1", Email: "alice@test.com", PasswordHash: "hash",
		Name: "Alice", Phone: "555", Role: "passenger", CreatedAt: 1000,
	}
	if err := s.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.GetByEmail(ctx, "alice@test.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.ID != u.ID || got.Name != u.Name || got.Role != u.Role {
		t.Errorf("got %+v, want %+v", got, u)
	}
}

func TestCreate_DuplicateEmail(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	u := &store.User{ID: "u_1", Email: "dupe@test.com", PasswordHash: "h", Role: "passenger", CreatedAt: 1}
	if err := s.Create(ctx, u); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	u2 := &store.User{ID: "u_2", Email: "dupe@test.com", PasswordHash: "h", Role: "passenger", CreatedAt: 2}
	if err := s.Create(ctx, u2); err != store.ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.GetByID(context.Background(), "no-such-id"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateProfile(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	u := &store.User{ID: "u_1", Email: "bob@test.com", PasswordHash: "h", Name: "Bob", Phone: "111", Role: "driver", CreatedAt: 1}
	_ = s.Create(ctx, u)

	if err := s.UpdateProfile(ctx, "u_1", "Robert", "999"); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	got, _ := s.GetByID(ctx, "u_1")
	if got.Name != "Robert" || got.Phone != "999" {
		t.Errorf("unexpected name/phone: %s / %s", got.Name, got.Phone)
	}
}

func TestUpdateProfile_NotFound(t *testing.T) {
	s := newStore(t)
	if err := s.UpdateProfile(context.Background(), "ghost", "X", "Y"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
