package service_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "ms/proto/gen/notification/v1"
	"ms/shared/testutil"
	"ms/notification-service/internal/hub"
	"ms/notification-service/internal/service"
	"ms/notification-service/internal/store"
)

func migrateNotifications(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newSvc(t *testing.T) *service.Server {
	t.Helper()
	db := testutil.NewMemDB(t, migrateNotifications)
	return service.New(store.New(db, slog.Default()), hub.New(), slog.Default())
}

func TestSendNotification_Success(t *testing.T) {
	svc := newSvc(t)
	resp, err := svc.SendNotification(context.Background(), &notificationv1.SendNotificationRequest{
		UserId: "u_1", Type: notificationv1.NotificationType_RIDE_MATCHED,
		Title: "Driver matched!", Body: "On the way.", RideId: "r_1",
	})
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}
	if resp.GetId() == "" {
		t.Error("expected non-empty notification id")
	}
}

func TestSendNotification_MissingUserID(t *testing.T) {
	svc := newSvc(t)
	_, err := svc.SendNotification(context.Background(), &notificationv1.SendNotificationRequest{
		Title: "oops",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestGetNotifications(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()

	for _, title := range []string{"A", "B", "C"} {
		_, _ = svc.SendNotification(ctx, &notificationv1.SendNotificationRequest{
			UserId: "u_1", Type: notificationv1.NotificationType_SYSTEM_MESSAGE,
			Title: title, Body: "body",
		})
	}

	resp, err := svc.GetNotifications(ctx, &notificationv1.GetNotificationsRequest{UserId: "u_1", Limit: 10})
	if err != nil {
		t.Fatalf("GetNotifications: %v", err)
	}
	if len(resp.GetNotifications()) != 3 {
		t.Fatalf("expected 3, got %d", len(resp.GetNotifications()))
	}
	if resp.GetUnreadCount() != 3 {
		t.Errorf("expected 3 unread, got %d", resp.GetUnreadCount())
	}
}

func TestMarkRead_All(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()

	for _, title := range []string{"X", "Y"} {
		_, _ = svc.SendNotification(ctx, &notificationv1.SendNotificationRequest{
			UserId: "u_1", Type: notificationv1.NotificationType_RIDE_COMPLETED, Title: title, Body: "b",
		})
	}

	markResp, err := svc.MarkRead(ctx, &notificationv1.MarkReadRequest{UserId: "u_1"})
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if markResp.GetMarked() != 2 {
		t.Errorf("expected 2 marked, got %d", markResp.GetMarked())
	}

	getResp, _ := svc.GetNotifications(ctx, &notificationv1.GetNotificationsRequest{UserId: "u_1", UnreadOnly: true})
	if getResp.GetUnreadCount() != 0 {
		t.Errorf("expected 0 unread, got %d", getResp.GetUnreadCount())
	}
}

func TestHub_LiveDelivery(t *testing.T) {
	h := hub.New()
	ch, unsub := h.Subscribe("u_1")
	defer unsub()

	db := testutil.NewMemDB(t, migrateNotifications)
	svc := service.New(store.New(db, slog.Default()), h, slog.Default())

	_, err := svc.SendNotification(context.Background(), &notificationv1.SendNotificationRequest{
		UserId: "u_1", Type: notificationv1.NotificationType_PAYMENT_CHARGED,
		Title: "Charged", Body: "$8.50",
	})
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}

	select {
	case n := <-ch:
		if n.GetTitle() != "Charged" {
			t.Errorf("unexpected notification: %+v", n)
		}
	default:
		t.Error("expected notification in hub channel, got none")
	}
}
