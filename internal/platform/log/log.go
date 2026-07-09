// Package log configures the process-wide structured logger.
package log

import (
	"log/slog"
	"os"
)

// New returns a JSON slog logger and installs it as the default.
func New(service string) *slog.Logger {
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).
		With("service", service)
	slog.SetDefault(l)
	return l
}
