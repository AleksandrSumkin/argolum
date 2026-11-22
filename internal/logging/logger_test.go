package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-logr/logr"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input       string
		expectedLvl slog.Level
		expectedOk  bool
		expectedStr string
	}{
		{"debug", slog.LevelDebug, true, "debug"},
		{"DEBUG", slog.LevelDebug, true, "debug"},
		{"  debug  ", slog.LevelDebug, true, "debug"},
		{"info", slog.LevelInfo, true, "info"},
		{"", slog.LevelInfo, true, "info"},
		{"warn", slog.LevelWarn, true, "warn"},
		{"warning", slog.LevelWarn, true, "warn"},
		{"error", slog.LevelError, true, "error"},
		{"dedug", slog.LevelInfo, false, "dedug"},
		{"invalid", slog.LevelInfo, false, "invalid"},
	}

	for _, tt := range tests {
		lvl, ok, str := ParseLevel(tt.input)
		if lvl != tt.expectedLvl {
			t.Errorf("ParseLevel(%q) level = %v, expected %v", tt.input, lvl, tt.expectedLvl)
		}
		if ok != tt.expectedOk {
			t.Errorf("ParseLevel(%q) ok = %v, expected %v", tt.input, ok, tt.expectedOk)
		}
		if str != tt.expectedStr {
			t.Errorf("ParseLevel(%q) str = %q, expected %q", tt.input, str, tt.expectedStr)
		}
	}
}

func TestSetup_Formats(t *testing.T) {
	// JSON format test
	loggerJSON := Setup("info", "json")
	if loggerJSON == nil {
		t.Fatal("Setup(info, json) returned nil")
	}

	// Text format test
	loggerText := Setup("debug", "text")
	if loggerText == nil {
		t.Fatal("Setup(debug, text) returned nil")
	}

	// Invalid level fallback test
	loggerFallback := Setup("dedug", "json")
	if loggerFallback == nil {
		t.Fatal("Setup(dedug, json) returned nil")
	}
}

func TestLogrIntegration(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)

	logrLogger := logr.FromSlogHandler(logger.Handler())
	if !logrLogger.Enabled() {
		t.Error("expected logrLogger to be enabled")
	}

	logrLogger.Info("test message from logr", "component", "controller-runtime")
	out := buf.String()
	if !strings.Contains(out, "test message from logr") {
		t.Errorf("expected buffer to contain message, got: %s", out)
	}
	if !strings.Contains(out, "controller-runtime") {
		t.Errorf("expected buffer to contain component key, got: %s", out)
	}
}

func TestSetup_LoggingOutput(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	logger.Log(context.Background(), slog.LevelDebug, "debug event")
	if !strings.Contains(buf.String(), "debug event") {
		t.Errorf("expected buffer to contain 'debug event', got: %s", buf.String())
	}
}
