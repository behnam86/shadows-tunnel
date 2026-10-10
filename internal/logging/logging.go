// Package logging provides structured JSON logging.
package logging

import (
	"log/slog"
	"os"
)

// New creates a JSON logger at the requested slog level.
func New(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
