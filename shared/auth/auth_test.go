package auth_test

import (
	"testing"
	"time"

	"ms/shared/auth"
)

const testSecret = "test-secret-key-123"

func TestIssueAndValidate(t *testing.T) {
	token, exp, err := auth.Issue(testSecret, "u_1", "passenger", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if exp.Before(time.Now()) {
		t.Error("expiry should be in the future")
	}

	claims, err := auth.Validate(testSecret, token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.UserID != "u_1" {
		t.Errorf("expected u_1, got %s", claims.UserID)
	}
	if claims.Role != "passenger" {
		t.Errorf("expected passenger, got %s", claims.Role)
	}
}

func TestValidate_WrongSecret(t *testing.T) {
	token, _, _ := auth.Issue(testSecret, "u_1", "driver", time.Hour)
	if _, err := auth.Validate("other-secret", token); err == nil {
		t.Error("expected error with wrong secret")
	}
}

func TestValidate_Expired(t *testing.T) {
	token, _, _ := auth.Issue(testSecret, "u_1", "passenger", -time.Second) // already expired
	if _, err := auth.Validate(testSecret, token); err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidate_Garbage(t *testing.T) {
	if _, err := auth.Validate(testSecret, "not.a.token"); err == nil {
		t.Error("expected error for garbage token")
	}
}
