// Package config has minimal env-var helpers shared across services.
package config

import "os"

// Env returns the environment variable value, or fallback if unset/empty.
func Env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
