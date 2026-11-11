// Package config loads user-service runtime configuration from environment.
package config

import (
	"errors"
	"time"

	msconfig "ms/shared/config"
)

// Config holds all user-service settings.
type Config struct {
	GRPCAddr   string
	DBPath     string
	JWTSecret  string
	JWTTTL     time.Duration
	LogLevel   string
}

// Load reads configuration from environment with sensible defaults.
// It returns an error if JWT_SECRET is not set in production.
func Load() Config {
	return Config{
		GRPCAddr:  msconfig.Env("GRPC_ADDR", ":50051"),
		DBPath:    msconfig.Env("DB_PATH", "data/user-service.db"),
		JWTSecret: msconfig.Env("JWT_SECRET", "super-secret-change-me"),
		JWTTTL:    24 * time.Hour,
		LogLevel:  msconfig.Env("LOG_LEVEL", "info"),
	}
}

// Validate returns an error if any required production config is missing or unsafe.
// In development (LOG_LEVEL=debug) the placeholder secret is allowed.
func (c Config) Validate() error {
	if c.LogLevel != "debug" && c.JWTSecret == "super-secret-change-me" {
		return errors.New("JWT_SECRET must be set to a strong secret in non-debug mode; set LOG_LEVEL=debug to allow the default during development")
	}
	if len(c.JWTSecret) < 16 {
		return errors.New("JWT_SECRET must be at least 16 characters")
	}
	return nil
}
