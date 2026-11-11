// Package service implements the gRPC UserService.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "ms/proto/gen/user/v1"
	"ms/shared/auth"
	"ms/user-service/internal/store"
)

// Server implements userv1.UserServiceServer.
type Server struct {
	userv1.UnimplementedUserServiceServer
	repo      *store.Store
	jwtSecret string
	jwtTTL    time.Duration
	log       *slog.Logger
}

// New creates a UserService gRPC server.
func New(repo *store.Store, jwtSecret string, jwtTTL time.Duration, log *slog.Logger) *Server {
	return &Server{repo: repo, jwtSecret: jwtSecret, jwtTTL: jwtTTL, log: log}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "u_" + hex.EncodeToString(b)
}

func roleToString(r userv1.UserRole) string {
	if r == userv1.UserRole_DRIVER {
		return "driver"
	}
	return "passenger"
}

func stringToRole(s string) userv1.UserRole {
	if s == "driver" {
		return userv1.UserRole_DRIVER
	}
	return userv1.UserRole_PASSENGER
}

// Register creates a new user account.
func (s *Server) Register(ctx context.Context, req *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	if req.GetEmail() == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.GetPassword()), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to hash password")
	}
	u := &store.User{
		ID:           newID(),
		Email:        req.GetEmail(),
		PasswordHash: string(hash),
		Name:         req.GetName(),
		Phone:        req.GetPhone(),
		Role:         roleToString(req.GetRole()),
		CreatedAt:    time.Now().Unix(),
	}
	if err := s.repo.Create(ctx, u); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		s.log.Error("create user", "err", err)
		return nil, status.Error(codes.Internal, "failed to create user")
	}
	s.log.Info("user registered", "id", u.ID, "email", u.Email, "role", u.Role)
	return &userv1.RegisterResponse{UserId: u.ID, Email: u.Email}, nil
}

// Login validates credentials and returns a JWT.
func (s *Server) Login(ctx context.Context, req *userv1.LoginRequest) (*userv1.LoginResponse, error) {
	u, err := s.repo.GetByEmail(ctx, req.GetEmail())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "invalid credentials")
		}
		return nil, status.Error(codes.Internal, "failed to look up user")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.GetPassword())) != nil {
		return nil, status.Error(codes.NotFound, "invalid credentials")
	}
	tok, exp, err := auth.Issue(s.jwtSecret, u.ID, u.Role, s.jwtTTL)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to issue token")
	}
	return &userv1.LoginResponse{Token: tok, UserId: u.ID, Role: stringToRole(u.Role), ExpiresAt: exp.Unix()}, nil
}

// GetProfile returns a user's profile.
func (s *Server) GetProfile(ctx context.Context, req *userv1.GetProfileRequest) (*userv1.UserProfile, error) {
	u, err := s.repo.GetByID(ctx, req.GetUserId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "failed to look up user")
	}
	return toProfile(u), nil
}

// UpdateProfile updates mutable profile fields.
func (s *Server) UpdateProfile(ctx context.Context, req *userv1.UpdateProfileRequest) (*userv1.UserProfile, error) {
	if err := s.repo.UpdateProfile(ctx, req.GetUserId(), req.GetName(), req.GetPhone()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "failed to update profile")
	}
	u, err := s.repo.GetByID(ctx, req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to reload user")
	}
	return toProfile(u), nil
}

func toProfile(u *store.User) *userv1.UserProfile {
	return &userv1.UserProfile{
		Id:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		Phone:     u.Phone,
		Role:      stringToRole(u.Role),
		CreatedAt: u.CreatedAt,
	}
}
