// Package logging monta o logger estruturado em JSON usado por toda a aplicação.
//
// Política de redaction (etapa 10): atributos cujo nome sugere credencial ou
// payload financeiro completo são substituídos por "[redacted]". Tokens,
// secrets, Authorization, corpos de aposta (money/amount/payload/body) não
// aparecem em claro. Identificadores de correlação e de negócio (ids, status,
// failureCode) permanecem visíveis para diagnóstico.
package logging

import (
	"log/slog"
	"os"
	"strings"

	"github.com/vigmi/backend-challenge-go/internal/config"
)

// New cria o logger JSON com o nível configurado e redaction de atributos sensíveis.
func New(cfg config.Config) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       levelOf(cfg.Log.Level),
		ReplaceAttr: RedactAttr,
	})
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

// RedactAttr oculta credenciais e payload financeiro completo no log JSON.
func RedactAttr(_ []string, a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	if shouldRedact(key) {
		return slog.String(a.Key, "[redacted]")
	}
	return a
}

func shouldRedact(key string) bool {
	switch key {
	case "authorization", "password", "secret", "token", "api_key", "apikey",
		"access_key", "accesskey", "secret_access_key", "secretaccesskey",
		"refresh_token", "id_token", "client_secret", "money", "amount",
		"payload", "body", "initialbalance":
		return true
	}
	if strings.Contains(key, "password") || strings.Contains(key, "secret") {
		return true
	}
	if strings.HasSuffix(key, "token") || strings.HasSuffix(key, "authorization") {
		return true
	}
	return false
}
