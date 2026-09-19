// Package logging monta o logger estruturado em JSON usado por toda a aplicação.
package logging

import (
	"log/slog"
	"os"

	"github.com/vigmi/backend-challenge-go/internal/config"
)

// New cria o logger JSON com o nível configurado.
func New(cfg config.Config) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: levelOf(cfg.Log.Level)})
	return slog.New(handler).With(
		slog.String("service", "wagering"),
		slog.String("env", cfg.Env),
	)
}

func levelOf(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
