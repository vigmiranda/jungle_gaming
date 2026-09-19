package httpserver

import (
	"net/http"

	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

// HealthHandler expõe os health checks públicos do item 9 do desafio.
type HealthHandler struct {
	checker *health.Checker
}

// NewHealthHandler cria o handler de health.
func NewHealthHandler(checker *health.Checker) *HealthHandler {
	return &HealthHandler{checker: checker}
}

// Live responde enquanto o processo estiver no ar.
func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, health.Report{Status: health.StatusOK})
}

// Ready responde 503 enquanto PostgreSQL ou SQS estiverem inacessíveis.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	report := h.checker.Check(r.Context())

	status := http.StatusOK
	if !report.Healthy() {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, report)
}
