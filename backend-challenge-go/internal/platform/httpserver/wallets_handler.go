package httpserver

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// WalletHandler expõe as rotas de carteira, restritas ao serviço interno.
type WalletHandler struct {
	open       *usecase.OpenWallet
	reconcile  *usecase.ReconcileWallet
	unitOfWork port.UnitOfWork
}

// NewWalletHandler monta o handler de carteiras.
func NewWalletHandler(
	open *usecase.OpenWallet,
	reconcile *usecase.ReconcileWallet,
	unitOfWork port.UnitOfWork,
) *WalletHandler {
	return &WalletHandler{open: open, reconcile: reconcile, unitOfWork: unitOfWork}
}

type openWalletRequest struct {
	PlayerID       string    `json:"playerId"`
	InitialBalance moneyBody `json:"initialBalance"`
}

// Open cria uma carteira.
func (h *WalletHandler) Open(w http.ResponseWriter, r *http.Request) {
	var body openWalletRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}

	result, err := h.open.Execute(r.Context(), usecase.OpenWalletCommand{
		PlayerID: body.PlayerID,
		InitialBalance: usecase.MoneyInput{
			Amount:   body.InitialBalance.Amount,
			Currency: body.InitialBalance.Currency,
		},
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, mapWallet(result.Wallet))
}

// Get devolve saldo e versão atuais.
func (h *WalletHandler) Get(w http.ResponseWriter, r *http.Request) {
	walletID, err := shared.ParseID(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, shared.Validation("INVALID_WALLET_ID", "walletId inválido").WithCause(err))
		return
	}

	var target *wallet.Wallet
	err = h.unitOfWork.ExecuteReadOnly(r.Context(), func(ctx context.Context, repositories port.Repositories) error {
		found, err := repositories.Wallets().FindByID(ctx, walletID)
		if err != nil {
			return err
		}
		target = found
		return nil
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, mapWallet(target))
}

// Ledger devolve o extrato paginado por cursor opaco.
func (h *WalletHandler) Ledger(w http.ResponseWriter, r *http.Request) {
	walletID, err := shared.ParseID(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, shared.Validation("INVALID_WALLET_ID", "walletId inválido").WithCause(err))
		return
	}

	cursor, err := decodeLedgerCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, err)
		return
	}

	limit, err := parseLimit(r.URL.Query().Get("limit"), 50, 200)
	if err != nil {
		writeError(w, err)
		return
	}

	var page port.LedgerPage
	err = h.unitOfWork.ExecuteReadOnly(r.Context(), func(ctx context.Context, repositories port.Repositories) error {
		if _, err := repositories.Wallets().FindByID(ctx, walletID); err != nil {
			return err
		}
		found, err := repositories.Ledger().ListByWallet(ctx, walletID, cursor, limit)
		if err != nil {
			return err
		}
		page = found
		return nil
	})
	if err != nil {
		writeError(w, err)
		return
	}

	response, err := mapLedgerPage(page)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// Reconcile compara saldo armazenado com o reconstruído do ledger.
func (h *WalletHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	walletID, err := shared.ParseID(chi.URLParam(r, "walletId"))
	if err != nil {
		writeError(w, shared.Validation("INVALID_WALLET_ID", "walletId inválido").WithCause(err))
		return
	}

	result, err := h.reconcile.Execute(r.Context(), walletID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, mapReconciliation(result))
}
