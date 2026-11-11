// Package config loads notification-service runtime configuration from environment.
package config

import msconfig "ms/shared/config"

// Config holds all notification-service settings.
type Config struct {
	GRPCAddr string
	DBPath   string
	NATSURL  string
	LogLevel string
}

// Load reads configuration from environment with sensible defaults.
func Load() Config {
	return Config{
		GRPCAddr: msconfig.Env("GRPC_ADDR", ":50056"),
		DBPath:   msconfig.Env("DB_PATH", "data/notification-service.db"),
		NATSURL:  msconfig.Env("NATS_URL", "nats://127.0.0.1:4222"),
		LogLevel: msconfig.Env("LOG_LEVEL", "info"),
	}
}
