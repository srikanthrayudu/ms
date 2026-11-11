package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"ms/notification-service/internal/store"
	"ms/shared/testutil"
)

func migrateNotifications(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(testutil.NewMemDB(t, migrateNotifications), slog.Default())
}

func TestCreate_and_List(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for i, id := range []string{"n_1", "n_2", "n_3"} {
		if err := s.Create(ctx, &store.Notification{
			ID: id, UserID: "u_1", Type: 1,
			Title: "title", Body: "body", RideID: "r_1",
			CreatedAt: int64(1000 + i),
		}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	// Different user
	_ = s.Create(ctx, &store.Notification{ID: "n_4", UserID: "u_2", Type: 1, Title: "t", Body: "b", CreatedAt: 999})

	ns, err := s.List(ctx, "u_1", false, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ns) != 3 {
		t.Fatalf("expected 3 notifications for u_1, got %d", len(ns))
	}
}

func TestCountUnread(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, id := range []string{"n_1", "n_2"} {
		_ = s.Create(ctx, &store.Notification{ID: id, UserID: "u_1", Type: 1, Title: "t", Body: "b", CreatedAt: 1000})
	}
	n, err := s.CountUnread(ctx, "u_1")
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 unread, got %d", n)
	}
}

func TestMarkRead_All(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ = s.Create(ctx, &store.Notification{ID: "n_1", UserID: "u_1", Type: 1, Title: "t", Body: "b", CreatedAt: 1000})
	_ = s.Create(ctx, &store.Notification{ID: "n_2", UserID: "u_1", Type: 1, Title: "t", Body: "b", CreatedAt: 1001})

	marked, err := s.MarkRead(ctx, "u_1", nil)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if marked != 2 {
		t.Fatalf("expected 2 marked, got %d", marked)
	}
	unread, _ := s.CountUnread(ctx, "u_1")
	if unread != 0 {
		t.Fatalf("expected 0 unread after MarkRead all, got %d", unread)
	}
}

func TestMarkRead_Specific(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ = s.Create(ctx, &store.Notification{ID: "n_1", UserID: "u_1", Type: 1, Title: "t", Body: "b", CreatedAt: 1000})
	_ = s.Create(ctx, &store.Notification{ID: "n_2", UserID: "u_1", Type: 1, Title: "t", Body: "b", CreatedAt: 1001})

	marked, err := s.MarkRead(ctx, "u_1", []string{"n_1"})
	if err != nil {
		t.Fatalf("MarkRead specific: %v", err)
	}
	if marked != 1 {
		t.Fatalf("expected 1 marked, got %d", marked)
	}
	unread, _ := s.CountUnread(ctx, "u_1")
	if unread != 1 {
		t.Fatalf("expected 1 still unread, got %d", unread)
	}
}

func TestList_UnreadOnly(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ = s.Create(ctx, &store.Notification{ID: "n_1", UserID: "u_1", Type: 1, Title: "t", Body: "b", CreatedAt: 1000})
	_ = s.Create(ctx, &store.Notification{ID: "n_2", UserID: "u_1", Type: 1, Title: "t", Body: "b", CreatedAt: 1001})
	_, _ = s.MarkRead(ctx, "u_1", []string{"n_1"})

	ns, err := s.List(ctx, "u_1", true, 10)
	if err != nil {
		t.Fatalf("List unread: %v", err)
	}
	if len(ns) != 1 || ns[0].ID != "n_2" {
		t.Errorf("expected only n_2, got %+v", ns)
	}
}
