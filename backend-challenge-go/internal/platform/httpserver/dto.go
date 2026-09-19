package httpserver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

type moneyBody struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type walletResponse struct {
	ID       string      `json:"id"`
	PlayerID string      `json:"playerId"`
	Balance  money.Money `json:"balance"`
	Version  int64       `json:"version"`
}

type transactionResponse struct {
	TransactionID                  string       `json:"transactionId"`
	Status                         string       `json:"status"`
	Kind                           string       `json:"kind,omitempty"`
	ProviderID                     string       `json:"providerId,omitempty"`
	ExternalTransactionID          string       `json:"externalTransactionId,omitempty"`
	PlayerID                       string       `json:"playerId,omitempty"`
	WalletID                       string       `json:"walletId,omitempty"`
	RoundID                        string       `json:"roundId,omitempty"`
	GameID                         string       `json:"gameId,omitempty"`
	ReferenceExternalTransactionID string       `json:"referenceExternalTransactionId,omitempty"`
	Money                          *money.Money `json:"money,omitempty"`
	Balance                        *money.Money `json:"balance,omitempty"`
	FailureCode                    string       `json:"failureCode,omitempty"`
	IdempotentReplay               bool         `json:"idempotentReplay"`
}

type reconciliationResponse struct {
	WalletID          string      `json:"walletId"`
	StoredBalance     money.Money `json:"storedBalance"`
	CalculatedBalance money.Money `json:"calculatedBalance"`
	Difference        money.Money `json:"difference"`
	Consistent        bool        `json:"consistent"`
	CheckedEntries    int         `json:"checkedEntries"`
}

type ledgerEntryResponse struct {
	ID            string      `json:"id"`
	WalletID      string      `json:"walletId"`
	TransactionID string      `json:"transactionId"`
	Direction     string      `json:"direction"`
	Amount        money.Money `json:"amount"`
	BalanceBefore money.Money `json:"balanceBefore"`
	BalanceAfter  money.Money `json:"balanceAfter"`
	CreatedAt     time.Time   `json:"createdAt"`
}

type ledgerPageResponse struct {
	Entries    []ledgerEntryResponse `json:"entries"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

func mapWallet(target *wallet.Wallet) walletResponse {
	return walletResponse{
		ID:       target.ID().String(),
		PlayerID: target.PlayerID().String(),
		Balance:  target.Balance(),
		Version:  target.Version(),
	}
}

func mapTransactionResult(result usecase.TransactionResult) transactionResponse {
	response := transactionResponse{
		TransactionID:    result.TransactionID.String(),
		Status:           result.Status.String(),
		FailureCode:      result.FailureCode.String(),
		IdempotentReplay: result.IdempotentReplay,
	}
	if result.HasBalance {
		balance := result.Balance
		response.Balance = &balance
	}
	return response
}

func mapTransaction(tx *wagering.Transaction) transactionResponse {
	amount := tx.Amount()
	response := transactionResponse{
		TransactionID:                  tx.ID().String(),
		Status:                         tx.Status().String(),
		Kind:                           tx.Kind().String(),
		ProviderID:                     tx.ProviderID(),
		ExternalTransactionID:          tx.ExternalID(),
		PlayerID:                       tx.PlayerID().String(),
		WalletID:                       tx.WalletID().String(),
		RoundID:                        tx.RoundID(),
		GameID:                         tx.GameID(),
		ReferenceExternalTransactionID: tx.ReferenceExternalID(),
		Money:                          &amount,
		FailureCode:                    tx.FailureCode().String(),
		IdempotentReplay:               false,
	}
	if result, ok := tx.Result(); ok {
		balance := result.Balance
		response.Balance = &balance
	}
	return response
}

func mapReconciliation(result usecase.ReconciliationResult) reconciliationResponse {
	return reconciliationResponse{
		WalletID:          result.WalletID.String(),
		StoredBalance:     result.StoredBalance,
		CalculatedBalance: result.CalculatedBalance,
		Difference:        result.Difference,
		Consistent:        result.Consistent,
		CheckedEntries:    result.CheckedEntries,
	}
}

func mapLedgerPage(page port.LedgerPage) (ledgerPageResponse, error) {
	entries := make([]ledgerEntryResponse, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, mapLedgerEntry(entry))
	}
	response := ledgerPageResponse{Entries: entries}
	if page.Next != nil {
		cursor, err := encodeLedgerCursor(*page.Next)
		if err != nil {
			return ledgerPageResponse{}, err
		}
		response.NextCursor = cursor
	}
	return response, nil
}

func mapLedgerEntry(entry ledger.Entry) ledgerEntryResponse {
	return ledgerEntryResponse{
		ID:            entry.ID().String(),
		WalletID:      entry.WalletID().String(),
		TransactionID: entry.TransactionID().String(),
		Direction:     entry.Direction().String(),
		Amount:        entry.Amount(),
		BalanceBefore: entry.BalanceBefore(),
		BalanceAfter:  entry.BalanceAfter(),
		CreatedAt:     entry.CreatedAt(),
	}
}

type cursorPayload struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

func encodeLedgerCursor(cursor port.LedgerCursor) (string, error) {
	raw, err := json.Marshal(cursorPayload{
		CreatedAt: cursor.CreatedAt.UTC(),
		ID:        cursor.ID.String(),
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeLedgerCursor(raw string) (*port.LedgerCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, shared.Validation("INVALID_CURSOR", "cursor inválido").WithCause(err)
	}
	var payload cursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, shared.Validation("INVALID_CURSOR", "cursor inválido").WithCause(err)
	}
	id, err := shared.ParseID(payload.ID)
	if err != nil {
		return nil, shared.Validation("INVALID_CURSOR", "cursor inválido").WithCause(err)
	}
	if payload.CreatedAt.IsZero() {
		return nil, shared.Validation("INVALID_CURSOR", "cursor inválido")
	}
	return &port.LedgerCursor{CreatedAt: payload.CreatedAt.UTC(), ID: id}, nil
}

func parseLimit(raw string, fallback, max int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > max {
		return 0, shared.Validation("INVALID_LIMIT",
			fmt.Sprintf("limit deve ser um inteiro entre 1 e %d", max))
	}
	return limit, nil
}
