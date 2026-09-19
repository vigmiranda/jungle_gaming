package httpserver

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
	"github.com/vigmi/backend-challenge-go/internal/platform/metrics"
)

// Correlation garante um identificador de rastreio por requisição, reaproveitando
// o recebido no cabeçalho quando houver (ADR-017).
func Correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(correlation.Header))
		if id == "" {
			id = correlation.NewID()
		}
		w.Header().Set(correlation.Header, id)
		next.ServeHTTP(w, r.WithContext(correlation.WithID(r.Context(), id)))
	})
}

// Recover converte um panic em resposta 500 sem derrubar o servidor.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				log.ErrorContext(r.Context(), "http_panic",
					slog.Any("panic", recovered),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("correlationId", correlation.FromContext(r.Context())),
				)
				writeJSON(w, http.StatusInternalServerError, errorBody{
					Error:   "internal_error",
					Message: "erro interno ao processar a requisição",
				})
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger registra cada requisição em JSON. Requisições de health e
// metrics ficam em nível debug para não poluir o log com as sondas.
func RequestLogger(log *slog.Logger, met *metrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			elapsed := time.Since(start)
			path := r.URL.Path
			quiet := strings.HasPrefix(path, "/health") || path == "/metrics"
			level := slog.LevelInfo
			if quiet {
				level = slog.LevelDebug
			}
			log.Log(r.Context(), level, "http_request",
				slog.String("method", r.Method),
				slog.String("path", path),
				slog.Int("status", recorder.status),
				slog.Duration("duration", elapsed),
				slog.String("correlationId", correlation.FromContext(r.Context())),
			)
			if met != nil && !quiet {
				met.RecordProcessingDuration("http", elapsed.Seconds())
			}
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}
