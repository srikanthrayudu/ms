// Package config loads ride-service runtime configuration from environment.
package config

import (
	msconfig "ms/shared/config"
)

// Config holds all ride-service settings.
type Config struct {
	GRPCAddr             string
	DBPath               string
	NATSURL              string
	LocationServiceAddr string
	DriverServiceAddr    string
	LogLevel             string
}

// Load reads configuration from environment with sensible defaults.
func Load() Config {
	return Config{
		GRPCAddr:             msconfig.Env("GRPC_ADDR", ":50054"),
		DBPath:               msconfig.Env("DB_PATH", "data/ride-service.db"),
		NATSURL:              msconfig.Env("NATS_URL", "nats://127.0.0.1:4222"),
		LocationServiceAddr: msconfig.Env("LOCATION_SERVICE_ADDR", "localhost:50053"),
		DriverServiceAddr:    msconfig.Env("DRIVER_SERVICE_ADDR", "localhost:50052"),
		LogLevel:             msconfig.Env("LOG_LEVEL", "info"),
	}
}
