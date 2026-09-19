package httpserver

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/vigmi/backend-challenge-go/internal/platform/auth"
)

// NewRouter monta o roteador da API.
//
// Health checks são públicos. Rotas de negócio exigem JWT válido; carteira e
// reconciliação ficam restritas ao serviço interno; operações de wagering ao
// client do provedor (com isolamento por providerId).
func NewRouter(
	log *slog.Logger,
	healthHandler *HealthHandler,
	authenticator *auth.Authenticator,
	wallets *WalletHandler,
	wagering *WageringHandler,
) *chi.Mux {
	router := chi.NewRouter()

	router.Use(Correlation)
	router.Use(Recover(log))
	router.Use(RequestLogger(log))

	router.Get("/health/live", healthHandler.Live)
	router.Get("/health/ready", healthHandler.Ready)

	if authenticator == nil || wallets == nil || wagering == nil {
		return router
	}

	router.Group(func(r chi.Router) {
		r.Use(authenticator.Middleware)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireInternal)
			r.Post("/wallets", wallets.Open)
			r.Get("/wallets/{walletId}", wallets.Get)
			r.Get("/wallets/{walletId}/ledger", wallets.Ledger)
			r.Post("/wallets/{walletId}/reconciliation", wallets.Reconcile)
		})

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireProvider)
			r.Post("/wagering/transactions", wagering.Create)
		})

		// Leituras de transação: interno ou provedor (filtrado no handler).
		r.Get("/wagering/transactions/{transactionId}", wagering.GetByID)
		r.Get("/providers/{providerId}/wagering/transactions/{externalTransactionId}", wagering.GetByExternalID)
	})

	return router
}
