// Package service implements the gRPC PaymentService.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	paymentv1 "ms/proto/gen/payment/v1"
	"ms/payment-service/internal/store"
)

// Server implements paymentv1.PaymentServiceServer.
type Server struct {
	paymentv1.UnimplementedPaymentServiceServer
	repo *store.Store
	log  *slog.Logger
}

// New creates a PaymentService gRPC server.
func New(repo *store.Store, log *slog.Logger) *Server {
	return &Server{repo: repo, log: log}
}

func newID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return "pay_" + hex.EncodeToString(b)
}

// ChargeRide creates or idempotently returns the payment for a ride.
func (s *Server) ChargeRide(ctx context.Context, req *paymentv1.ChargeRideRequest) (*paymentv1.Payment, error) {
	if req.GetRideId() == "" {
		return nil, status.Error(codes.InvalidArgument, "ride_id is required")
	}
	now := time.Now().Unix()
	p, err := s.repo.Create(ctx, &store.Payment{
		ID:          newID(),
		RideID:      req.GetRideId(),
		PassengerID: req.GetPassengerId(),
		DriverID:    req.GetDriverId(),
		Amount:      req.GetAmount(),
		Status:      int(paymentv1.PaymentStatus_COMPLETED),
		CreatedAt:   now,
		SettledAt:   now,
		Notes:       "auto-charged on ride completion",
	})
	if err != nil {
		s.log.Error("charge ride", "err", err)
		return nil, status.Error(codes.Internal, "failed to charge ride")
	}
	s.log.Info("ride charged", "ride_id", p.RideID, "amount", p.Amount)
	return toProto(p), nil
}

// GetPayment returns the payment for a ride.
func (s *Server) GetPayment(ctx context.Context, req *paymentv1.GetPaymentRequest) (*paymentv1.Payment, error) {
	p, err := s.repo.GetByRideID(ctx, req.GetRideId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "payment not found")
		}
		return nil, status.Error(codes.Internal, "failed to get payment")
	}
	return toProto(p), nil
}

// ListPayments returns all payments for a user.
func (s *Server) ListPayments(ctx context.Context, req *paymentv1.ListPaymentsRequest) (*paymentv1.ListPaymentsResponse, error) {
	payments, total, err := s.repo.ListByUser(ctx, req.GetUserId(), req.GetRole(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list payments")
	}
	protos := make([]*paymentv1.Payment, len(payments))
	for i, p := range payments {
		protos[i] = toProto(p)
	}
	return &paymentv1.ListPaymentsResponse{Payments: protos, Total: total}, nil
}

func toProto(p *store.Payment) *paymentv1.Payment {
	return &paymentv1.Payment{
		Id:          p.ID,
		RideId:      p.RideID,
		PassengerId: p.PassengerID,
		DriverId:    p.DriverID,
		Amount:      p.Amount,
		Status:      paymentv1.PaymentStatus(p.Status),
		CreatedAt:   p.CreatedAt,
		SettledAt:   p.SettledAt,
		Notes:       p.Notes,
	}
}
