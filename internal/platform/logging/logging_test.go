package logging_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/logging"
)

func TestNewBuildsJSONLogger(t *testing.T) {
	logger := logging.New(config.Config{Env: config.EnvTest, Log: config.Log{Level: "debug"}})
	if logger == nil {
		t.Fatal("logger nil")
	}
}

func TestRedactAttrHidesSecretsAndMoney(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: logging.RedactAttr,
	}))
	logger.Info("probe",
		slog.String("authorization", "Bearer x"),
		slog.String("accessToken", "tok"),
		slog.String("money", "99.00"),
		slog.String("correlationId", "corr-1"),
		slog.String("failureCode", "INSUFFICIENT_FUNDS"),
	)

	out := buf.String()
	if strings.Contains(out, "Bearer x") || strings.Contains(out, `"tok"`) || strings.Contains(out, "99.00") {
		t.Fatalf("valores sensíveis vazaram: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("esperava [redacted]: %s", out)
	}
	if !strings.Contains(out, "corr-1") || !strings.Contains(out, "INSUFFICIENT_FUNDS") {
		t.Fatalf("campos de diagnóstico sumiram: %s", out)
	}
}
