// Package log provides a tiny wrapper around log/slog so every service
// emits structured logs in the same shape.
package log

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a text slog logger tagged with the service name.
func New(svc string, level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h).With("svc", svc)
}
