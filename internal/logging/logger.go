// Package logging provides structured logging configuration and controller-runtime integration.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// ParseLevel converts a level string into a slog.Level, returning the effective level,
// a boolean indicating if it was recognized, and the normalized level string.
func ParseLevel(levelStr string) (slog.Level, bool, string) {
	clean := strings.ToLower(strings.TrimSpace(levelStr))
	switch clean {
	case "debug":
		return slog.LevelDebug, true, "debug"
	case "info", "":
		return slog.LevelInfo, true, "info"
	case "warn", "warning":
		return slog.LevelWarn, true, "warn"
	case "error":
		return slog.LevelError, true, "error"
	default:
		return slog.LevelInfo, false, clean
	}
}

// Setup initializes the global slog logger based on LOG_LEVEL and LOG_FORMAT.
func Setup(levelStr, formatStr string) *slog.Logger {
	level, ok, _ := ParseLevel(levelStr)

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if strings.ToLower(strings.TrimSpace(formatStr)) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	if !ok {
		logger.Warn("Unrecognized LOG_LEVEL, falling back to INFO",
			slog.String("configuredLogLevel", levelStr),
			slog.String("effectiveLogLevel", "info"),
		)
	}

	return logger
}
