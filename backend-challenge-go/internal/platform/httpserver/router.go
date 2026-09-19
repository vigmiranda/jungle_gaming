package httpserver

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
)

// NewRouter monta o roteador da API.
//
// As rotas de negócio entram nas etapas seguintes; por ora só os health checks
// públicos, que não exigem autenticação.
func NewRouter(log *slog.Logger, healthHandler *HealthHandler) *chi.Mux {
	router := chi.NewRouter()

	router.Use(Correlation)
	router.Use(Recover(log))
	router.Use(RequestLogger(log))

	router.Get("/health/live", healthHandler.Live)
	router.Get("/health/ready", healthHandler.Ready)

	return router
}
