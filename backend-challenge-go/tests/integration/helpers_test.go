//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testTimeout = 10 * time.Second

func newUUID(t *testing.T) uuid.UUID {
	t.Helper()
	generated, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("não foi possível gerar UUID: %v", err)
	}
	return generated
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// walletRow descreve uma carteira a inserir.
type walletRow struct {
	ID       uuid.UUID
	PlayerID uuid.UUID
	Currency string
	Balance  int64
	Version  int64
}

func newWalletRow(t *testing.T) walletRow {
	t.Helper()
	return walletRow{
		ID:       newUUID(t),
		PlayerID: newUUID(t),
		Currency: "BRL",
		Balance:  100000,
		Version:  1,
	}
}

func insertWallet(ctx context.Context, row walletRow) error {
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		INSERT INTO wallets (id, player_id, currency, balance_minor, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		row.ID, row.PlayerID, row.Currency, row.Balance, row.Version, now)
	return err
}

func mustInsertWallet(t *testing.T, ctx context.Context) walletRow {
	t.Helper()
	row := newWalletRow(t)
	if err := insertWallet(ctx, row); err != nil {
		t.Fatalf("não foi possível inserir a carteira: %v", err)
	}
	return row
}

// transactionRow descreve uma transação a inserir. Os campos opcionais usam
// ponteiro para distinguir ausência de valor vazio, como no schema.
type transactionRow struct {
	ID                  uuid.UUID
	Origin              string
	Kind                string
	Status              string
	WalletID            uuid.UUID
	PlayerID            uuid.UUID
	AmountMinor         int64
	Currency            string
	ProviderID          *string
	ExternalID          *string
	IdempotencyKey      *string
	PayloadHash         *string
	RoundID             *string
	GameID              *string
	ReferenceExternalID *string
	ReferenceID         *uuid.UUID
	FailureCode         *string
	ResultBalanceMinor  *int64
	ResultCurrency      *string
	ResultVersion       *int64
}

func text(value string) *string { return &value }

func number(value int64) *int64 { return &value }

// newExternalTransaction monta uma BET processada sobre a carteira informada.
func newExternalTransaction(t *testing.T, wallet walletRow) transactionRow {
	t.Helper()
	externalID := "transaction-" + newUUID(t).String()

	return transactionRow{
		ID:                 newUUID(t),
		Origin:             "EXTERNAL",
		Kind:               "BET",
		Status:             "PROCESSED",
		WalletID:           wallet.ID,
		PlayerID:           wallet.PlayerID,
		AmountMinor:        2500,
		Currency:           wallet.Currency,
		ProviderID:         text("provider-a"),
		ExternalID:         text(externalID),
		IdempotencyKey:     text("provider-a:" + externalID),
		PayloadHash:        text("3f786850e387550fdab836ed7e6dc881de23001b"),
		RoundID:            text("round-987"),
		GameID:             text("fortune-chimp"),
		ResultBalanceMinor: number(wallet.Balance - 2500),
		ResultCurrency:     text(wallet.Currency),
		ResultVersion:      number(wallet.Version + 1),
	}
}

// newOpeningTransaction monta a abertura interna da carteira informada.
func newOpeningTransaction(t *testing.T, wallet walletRow) transactionRow {
	t.Helper()
	return transactionRow{
		ID:                 newUUID(t),
		Origin:             "INTERNAL",
		Kind:               "OPENING",
		Status:             "PROCESSED",
		WalletID:           wallet.ID,
		PlayerID:           wallet.PlayerID,
		AmountMinor:        wallet.Balance,
		Currency:           wallet.Currency,
		ResultBalanceMinor: number(wallet.Balance),
		ResultCurrency:     text(wallet.Currency),
		ResultVersion:      number(wallet.Version),
	}
}

func insertTransaction(ctx context.Context, row transactionRow) error {
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		INSERT INTO wager_transactions (
			id, origin, kind, status, wallet_id, player_id, amount_minor, amount_currency,
			provider_id, external_transaction_id, idempotency_key, payload_hash,
			round_id, game_id, reference_external_transaction_id, reference_transaction_id,
			failure_code, result_balance_minor, result_balance_currency, result_wallet_version,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19, $20,
			$21, $21
		)`,
		row.ID, row.Origin, row.Kind, row.Status, row.WalletID, row.PlayerID, row.AmountMinor, row.Currency,
		row.ProviderID, row.ExternalID, row.IdempotencyKey, row.PayloadHash,
		row.RoundID, row.GameID, row.ReferenceExternalID, row.ReferenceID,
		row.FailureCode, row.ResultBalanceMinor, row.ResultCurrency, row.ResultVersion,
		now)
	return err
}

func mustInsertTransaction(t *testing.T, ctx context.Context, row transactionRow) transactionRow {
	t.Helper()
	if err := insertTransaction(ctx, row); err != nil {
		t.Fatalf("não foi possível inserir a transação: %v", err)
	}
	return row
}

// ledgerRow descreve um lançamento a inserir.
type ledgerRow struct {
	ID            uuid.UUID
	WalletID      uuid.UUID
	TransactionID uuid.UUID
	Direction     string
	AmountMinor   int64
	Currency      string
	BalanceBefore int64
	BalanceAfter  int64
}

func newLedgerRow(t *testing.T, wallet walletRow, transactionID uuid.UUID) ledgerRow {
	t.Helper()
	return ledgerRow{
		ID:            newUUID(t),
		WalletID:      wallet.ID,
		TransactionID: transactionID,
		Direction:     "DEBIT",
		AmountMinor:   2500,
		Currency:      wallet.Currency,
		BalanceBefore: wallet.Balance,
		BalanceAfter:  wallet.Balance - 2500,
	}
}

func insertLedgerEntry(ctx context.Context, row ledgerRow) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO wallet_ledger_entries (
			id, wallet_id, transaction_id, direction, amount_minor, currency,
			balance_before_minor, balance_after_minor, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		row.ID, row.WalletID, row.TransactionID, row.Direction, row.AmountMinor, row.Currency,
		row.BalanceBefore, row.BalanceAfter, time.Now().UTC())
	return err
}

func mustInsertLedgerEntry(t *testing.T, ctx context.Context, row ledgerRow) ledgerRow {
	t.Helper()
	if err := insertLedgerEntry(ctx, row); err != nil {
		t.Fatalf("não foi possível inserir o lançamento: %v", err)
	}
	return row
}
