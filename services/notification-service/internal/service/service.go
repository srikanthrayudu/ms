// Package service implements the gRPC NotificationService.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "ms/proto/gen/notification/v1"
	"ms/notification-service/internal/hub"
	"ms/notification-service/internal/store"
)

// Server implements notificationv1.NotificationServiceServer.
type Server struct {
	notificationv1.UnimplementedNotificationServiceServer
	repo *store.Store
	hub  *hub.Hub
	log  *slog.Logger
}

// New creates a NotificationService gRPC server.
func New(repo *store.Store, h *hub.Hub, log *slog.Logger) *Server {
	return &Server{repo: repo, hub: h, log: log}
}

func newID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return "notif_" + hex.EncodeToString(b)
}

// SendNotification creates a notification and fans it out to live SSE subscribers.
func (s *Server) SendNotification(ctx context.Context, req *notificationv1.SendNotificationRequest) (*notificationv1.SendNotificationResponse, error) {
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	n := &store.Notification{
		ID:        newID(),
		UserID:    req.GetUserId(),
		Type:      int(req.GetType()),
		Title:     req.GetTitle(),
		Body:      req.GetBody(),
		RideID:    req.GetRideId(),
		CreatedAt: time.Now().Unix(),
	}
	if err := s.repo.Create(ctx, n); err != nil {
		s.log.Error("create notification", "err", err)
		return nil, status.Error(codes.Internal, "failed to create notification")
	}

	// Fan out to live subscribers (best-effort, non-blocking).
	s.hub.Publish(req.GetUserId(), toProto(n))
	s.log.Info("notification sent", "user_id", n.UserID, "type", n.Type, "title", n.Title)
	return &notificationv1.SendNotificationResponse{Id: n.ID}, nil
}

// GetNotifications returns recent notifications for a user.
func (s *Server) GetNotifications(ctx context.Context, req *notificationv1.GetNotificationsRequest) (*notificationv1.GetNotificationsResponse, error) {
	ns, err := s.repo.List(ctx, req.GetUserId(), req.GetUnreadOnly(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list notifications")
	}
	unread, _ := s.repo.CountUnread(ctx, req.GetUserId())
	protos := make([]*notificationv1.Notification, len(ns))
	for i, n := range ns {
		protos[i] = toProto(n)
	}
	return &notificationv1.GetNotificationsResponse{
		Notifications: protos,
		UnreadCount:   unread,
	}, nil
}

// MarkRead marks notifications as read.
func (s *Server) MarkRead(ctx context.Context, req *notificationv1.MarkReadRequest) (*notificationv1.MarkReadResponse, error) {
	n, err := s.repo.MarkRead(ctx, req.GetUserId(), req.GetNotificationIds())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to mark notifications read")
	}
	return &notificationv1.MarkReadResponse{Marked: n}, nil
}

func toProto(n *store.Notification) *notificationv1.Notification {
	return &notificationv1.Notification{
		Id:        n.ID,
		UserId:    n.UserID,
		Type:      notificationv1.NotificationType(n.Type),
		Title:     n.Title,
		Body:      n.Body,
		RideId:    n.RideID,
		Read:      n.Read,
		CreatedAt: n.CreatedAt,
	}
}
