// Package config loads the api-gateway runtime configuration from environment.
package config

import (
	"errors"

	msconfig "ms/shared/config"
)

// Config holds all api-gateway settings.
type Config struct {
	HTTPAddr                string
	UserServiceAddr         string
	DriverServiceAddr       string
	LocationServiceAddr     string
	RideServiceAddr         string
	PaymentServiceAddr      string
	NotificationServiceAddr string
	JWTSecret               string
	NATSURL                 string
	LogLevel                string
}

// Load reads configuration from environment with sensible defaults.
func Load() Config {
	return Config{
		HTTPAddr:                msconfig.Env("HTTP_ADDR", ":8080"),
		UserServiceAddr:         msconfig.Env("USER_SERVICE_ADDR", "localhost:50051"),
		DriverServiceAddr:       msconfig.Env("DRIVER_SERVICE_ADDR", "localhost:50052"),
		LocationServiceAddr:     msconfig.Env("LOCATION_SERVICE_ADDR", "localhost:50053"),
		RideServiceAddr:         msconfig.Env("RIDE_SERVICE_ADDR", "localhost:50054"),
		PaymentServiceAddr:      msconfig.Env("PAYMENT_SERVICE_ADDR", "localhost:50055"),
		NotificationServiceAddr: msconfig.Env("NOTIFICATION_SERVICE_ADDR", "localhost:50056"),
		JWTSecret:               msconfig.Env("JWT_SECRET", "super-secret-change-me"),
		NATSURL:                 msconfig.Env("NATS_URL", "nats://127.0.0.1:4222"),
		LogLevel:                msconfig.Env("LOG_LEVEL", "info"),
	}
}

// Validate returns an error if any required production config is missing or unsafe.
func (c Config) Validate() error {
	if c.LogLevel != "debug" && c.JWTSecret == "super-secret-change-me" {
		return errors.New("JWT_SECRET must be set to a strong secret in non-debug mode; set LOG_LEVEL=debug to allow the default during development")
	}
	if len(c.JWTSecret) < 16 {
		return errors.New("JWT_SECRET must be at least 16 characters")
	}
	return nil
}
