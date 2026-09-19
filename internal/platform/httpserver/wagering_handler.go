package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/platform/auth"
	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
)

// WageringHandler expõe as rotas de operações de provedor.
type WageringHandler struct {
	process    *usecase.ProcessWagerTransaction
	unitOfWork port.UnitOfWork
	log        *slog.Logger
	recorder   port.Recorder
}

// NewWageringHandler monta o handler de wagering.
func NewWageringHandler(
	process *usecase.ProcessWagerTransaction,
	unitOfWork port.UnitOfWork,
	log *slog.Logger,
	recorder port.Recorder,
) *WageringHandler {
	return &WageringHandler{process: process, unitOfWork: unitOfWork, log: log, recorder: recorder}
}

type processTransactionRequest struct {
	ProviderID                     string    `json:"providerId"`
	ExternalTransactionID          string    `json:"externalTransactionId"`
	PlayerID                       string    `json:"playerId"`
	WalletID                       string    `json:"walletId"`
	RoundID                        string    `json:"roundId"`
	GameID                         string    `json:"gameId"`
	Kind                           string    `json:"kind"`
	Money                          moneyBody `json:"money"`
	ReferenceExternalTransactionID string    `json:"referenceExternalTransactionId"`
}

// Create processa uma operação financeira do provedor.
func (h *WageringHandler) Create(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.RequireIdentity(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, shared.Validation("MISSING_IDEMPOTENCY_KEY", "header Idempotency-Key é obrigatório"))
		return
	}

	var body processTransactionRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}

	if body.ProviderID != identity.ProviderID {
		writeError(w, auth.ErrForbidden)
		return
	}

	start := time.Now()
	result, err := h.process.Execute(r.Context(), usecase.ProcessTransactionCommand{
		IdempotencyKey:                 idempotencyKey,
		ProviderID:                     body.ProviderID,
		ExternalTransactionID:          body.ExternalTransactionID,
		PlayerID:                       body.PlayerID,
		WalletID:                       body.WalletID,
		RoundID:                        body.RoundID,
		GameID:                         body.GameID,
		Kind:                           body.Kind,
		Money:                          usecase.MoneyInput{Amount: body.Money.Amount, Currency: body.Money.Currency},
		ReferenceExternalTransactionID: body.ReferenceExternalTransactionID,
	})
	if err != nil {
		writeErrorWith(w, err, h.recorder)
		return
	}

	if h.recorder != nil {
		h.recorder.RecordTransaction("http", string(result.Status), result.IdempotentReplay)
		h.recorder.RecordProcessingDuration("http_usecase", time.Since(start).Seconds())
	}
	if h.log != nil {
		attrs := []any{
			slog.String("correlationId", correlation.FromContext(r.Context())),
			slog.String("transactionId", result.TransactionID.String()),
			slog.String("walletId", body.WalletID),
			slog.String("providerId", body.ProviderID),
			slog.String("status", string(result.Status)),
			slog.Bool("idempotentReplay", result.IdempotentReplay),
		}
		if result.FailureCode != "" {
			attrs = append(attrs, slog.String("failureCode", string(result.FailureCode)))
		}
		h.log.InfoContext(r.Context(), "wager_transaction_processed", attrs...)
	}

	writeJSON(w, statusForTransaction(result.Status), mapTransactionResult(result))
}

// GetByID consulta uma operação pelo identificador interno.
//
// Serviço interno vê qualquer transação. Provedor só vê as próprias.
func (h *WageringHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.RequireIdentity(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	transactionID, err := shared.ParseID(chi.URLParam(r, "transactionId"))
	if err != nil {
		writeError(w, shared.Validation("INVALID_TRANSACTION_ID", "transactionId inválido").WithCause(err))
		return
	}

	tx, err := h.loadByID(r.Context(), transactionID)
	if err != nil {
		writeError(w, err)
		return
	}

	if err := authorizeTransactionRead(identity, tx); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, mapTransaction(tx))
}

// GetByExternalID consulta pela chave do provedor.
func (h *WageringHandler) GetByExternalID(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.RequireIdentity(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	providerID := chi.URLParam(r, "providerId")
	externalID := chi.URLParam(r, "externalTransactionId")
	if providerID == "" || externalID == "" {
		writeError(w, shared.Validation("INVALID_INPUT", "providerId e externalTransactionId são obrigatórios"))
		return
	}

	if err := authorizeProviderPath(identity, providerID); err != nil {
		writeError(w, err)
		return
	}

	tx, err := h.loadByExternalID(r.Context(), providerID, externalID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, mapTransaction(tx))
}

func (h *WageringHandler) loadByID(ctx context.Context, id shared.ID) (*wagering.Transaction, error) {
	var tx *wagering.Transaction
	err := h.unitOfWork.ExecuteReadOnly(ctx, func(ctx context.Context, repositories port.Repositories) error {
		found, err := repositories.Transactions().FindByID(ctx, id)
		if err != nil {
			return err
		}
		tx = found
		return nil
	})
	return tx, err
}

func (h *WageringHandler) loadByExternalID(
	ctx context.Context,
	providerID, externalID string,
) (*wagering.Transaction, error) {
	var tx *wagering.Transaction
	err := h.unitOfWork.ExecuteReadOnly(ctx, func(ctx context.Context, repositories port.Repositories) error {
		found, err := repositories.Transactions().FindByExternalID(ctx, providerID, externalID)
		if err != nil {
			return err
		}
		tx = found
		return nil
	})
	return tx, err
}

func authorizeTransactionRead(identity auth.Identity, tx *wagering.Transaction) error {
	switch identity.Role {
	case auth.RoleInternal:
		return nil
	case auth.RoleProvider:
		if tx.Origin() != wagering.OriginExternal || tx.ProviderID() != identity.ProviderID {
			return auth.ErrForbidden
		}
		return nil
	default:
		return auth.ErrForbidden
	}
}

func authorizeProviderPath(identity auth.Identity, providerID string) error {
	switch identity.Role {
	case auth.RoleInternal:
		return nil
	case auth.RoleProvider:
		if identity.ProviderID != providerID {
			return auth.ErrForbidden
		}
		return nil
	default:
		return auth.ErrForbidden
	}
}

func statusForTransaction(status wagering.Status) int {
	switch status {
	case wagering.PendingReference:
		return http.StatusAccepted
	case wagering.Rejected:
		return http.StatusUnprocessableEntity
	case wagering.Failed:
		return http.StatusServiceUnavailable
	default:
		return http.StatusOK
	}
}
