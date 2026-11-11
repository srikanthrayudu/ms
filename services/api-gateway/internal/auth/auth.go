// Package auth provides request-scoped user identity for the gateway.
package auth

import (
	"context"
	"net/http"

	msauth "ms/shared/auth"
)

// User is the authenticated identity made available to handlers.
type User struct {
	ID   string
	Role string
}

type ctxKey struct{}

// WithUser stores the user in the request context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// FromContext returns the user in the context, or nil if anonymous.
func FromContext(ctx context.Context) *User {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(ctxKey{}).(*User)
	return v
}

// FromRequest extracts the user from the "token" cookie, validating the JWT.
func FromRequest(r *http.Request, secret string) *User {
	c, err := r.Cookie("token")
	if err != nil {
		return nil
	}
	claims, err := msauth.Validate(secret, c.Value)
	if err != nil {
		return nil
	}
	return &User{ID: claims.UserID, Role: claims.Role}
}
