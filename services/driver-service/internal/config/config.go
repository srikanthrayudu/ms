// Package config loads driver-service runtime configuration from environment.
package config

import (
	msconfig "ms/shared/config"
)

// Config holds all driver-service settings.
type Config struct {
	GRPCAddr string
	DBPath   string
	LogLevel string
}

// Load reads configuration from environment with sensible defaults.
func Load() Config {
	return Config{
		GRPCAddr: msconfig.Env("GRPC_ADDR", ":50052"),
		DBPath:   msconfig.Env("DB_PATH", "data/driver-service.db"),
		LogLevel: msconfig.Env("LOG_LEVEL", "info"),
	}
}
