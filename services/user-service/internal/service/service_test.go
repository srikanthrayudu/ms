package service_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "ms/proto/gen/user/v1"
	"ms/shared/testutil"
	"ms/user-service/internal/service"
	"ms/user-service/internal/store"
)

func migrateUsers(_ context.Context, db *sql.DB) error {
	return store.New(db, slog.Default()).Migrate(context.Background())
}

func newSvc(t *testing.T) *service.Server {
	t.Helper()
	db := testutil.NewMemDB(t, migrateUsers)
	repo := store.New(db, slog.Default())
	return service.New(repo, "test-secret", time.Hour, slog.Default())
}

func TestRegister_Success(t *testing.T) {
	svc := newSvc(t)
	resp, err := svc.Register(context.Background(), &userv1.RegisterRequest{
		Email: "alice@test.com", Password: "pw123", Name: "Alice",
		Phone: "555", Role: userv1.UserRole_PASSENGER,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.GetEmail() != "alice@test.com" {
		t.Errorf("unexpected email: %s", resp.GetEmail())
	}
	if resp.GetUserId() == "" {
		t.Error("user_id should be set")
	}
}

func TestRegister_MissingEmail(t *testing.T) {
	svc := newSvc(t)
	_, err := svc.Register(context.Background(), &userv1.RegisterRequest{Password: "pw"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	svc := newSvc(t)
	req := &userv1.RegisterRequest{Email: "dupe@test.com", Password: "pw", Name: "X", Role: userv1.UserRole_PASSENGER}
	if _, err := svc.Register(context.Background(), req); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := svc.Register(context.Background(), req)
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists, got %v", err)
	}
}

func TestLogin_Success(t *testing.T) {
	svc := newSvc(t)
	_, _ = svc.Register(context.Background(), &userv1.RegisterRequest{
		Email: "bob@test.com", Password: "secret", Name: "Bob", Role: userv1.UserRole_DRIVER,
	})
	resp, err := svc.Login(context.Background(), &userv1.LoginRequest{
		Email: "bob@test.com", Password: "secret",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.GetToken() == "" {
		t.Error("expected non-empty token")
	}
	if resp.GetRole() != userv1.UserRole_DRIVER {
		t.Errorf("expected DRIVER role, got %v", resp.GetRole())
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc := newSvc(t)
	_, _ = svc.Register(context.Background(), &userv1.RegisterRequest{
		Email: "eve@test.com", Password: "correct", Name: "Eve", Role: userv1.UserRole_PASSENGER,
	})
	_, err := svc.Login(context.Background(), &userv1.LoginRequest{
		Email: "eve@test.com", Password: "wrong",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound for wrong password, got %v", err)
	}
}

func TestGetProfile(t *testing.T) {
	svc := newSvc(t)
	reg, _ := svc.Register(context.Background(), &userv1.RegisterRequest{
		Email: "charlie@test.com", Password: "pw", Name: "Charlie", Role: userv1.UserRole_PASSENGER,
	})
	profile, err := svc.GetProfile(context.Background(), &userv1.GetProfileRequest{UserId: reg.GetUserId()})
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if profile.GetName() != "Charlie" {
		t.Errorf("expected Charlie, got %s", profile.GetName())
	}
}

func TestGetProfile_NotFound(t *testing.T) {
	svc := newSvc(t)
	_, err := svc.GetProfile(context.Background(), &userv1.GetProfileRequest{UserId: "nobody"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestUpdateProfile(t *testing.T) {
	svc := newSvc(t)
	reg, _ := svc.Register(context.Background(), &userv1.RegisterRequest{
		Email: "dana@test.com", Password: "pw", Name: "Dana", Role: userv1.UserRole_PASSENGER,
	})
	profile, err := svc.UpdateProfile(context.Background(), &userv1.UpdateProfileRequest{
		UserId: reg.GetUserId(), Name: "Dana Updated", Phone: "999",
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if profile.GetName() != "Dana Updated" || profile.GetPhone() != "999" {
		t.Errorf("unexpected profile: %+v", profile)
	}
}
