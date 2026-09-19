package logging_test

import (
	"log/slog"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/logging"
)

func TestNewRespectsConfiguredLevel(t *testing.T) {
	tests := []struct {
		level   string
		enabled slog.Level
		blocked slog.Level
	}{
		{"debug", slog.LevelDebug, slog.LevelDebug - 1},
		{"info", slog.LevelInfo, slog.LevelDebug},
		{"warn", slog.LevelWarn, slog.LevelInfo},
		{"error", slog.LevelError, slog.LevelWarn},
		{"desconhecido", slog.LevelInfo, slog.LevelDebug},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger := logging.New(config.Config{Env: config.EnvTest, Log: config.Log{Level: tt.level}})

			if !logger.Enabled(t.Context(), tt.enabled) {
				t.Errorf("nível %s deveria estar habilitado para %q", tt.enabled, tt.level)
			}
			if logger.Enabled(t.Context(), tt.blocked) {
				t.Errorf("nível %s não deveria estar habilitado para %q", tt.blocked, tt.level)
			}
		})
	}
}
