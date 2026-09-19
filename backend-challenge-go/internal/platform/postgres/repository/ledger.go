package repository

import (
	"context"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// LedgerRepository registra e lê os lançamentos da carteira.
type LedgerRepository struct {
	db querier
}

const ledgerColumns = `
	id, wallet_id, transaction_id, direction, amount_minor, currency,
	balance_before_minor, balance_after_minor, created_at`

// Append grava o lançamento. O ledger é append-only: não há update nem delete.
func (r *LedgerRepository) Append(ctx context.Context, entry ledger.Entry) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wallet_ledger_entries (`+ledgerColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		entry.ID().String(),
		entry.WalletID().String(),
		entry.TransactionID().String(),
		entry.Direction().String(),
		entry.Amount().MinorUnits(),
		entry.Amount().Currency().String(),
		entry.BalanceBefore().MinorUnits(),
		entry.BalanceAfter().MinorUnits(),
		entry.CreatedAt(),
	)
	return translate("registrar lançamento", err)
}

// ListByWallet devolve uma página ordenada por `(created_at, id)`.
//
// A paginação é por cursor, nunca por offset: com lançamentos chegando durante
// a navegação, o offset repetiria ou pularia linhas.
func (r *LedgerRepository) ListByWallet(
	ctx context.Context,
	walletID shared.ID,
	after *port.LedgerCursor,
	limit int,
) (port.LedgerPage, error) {
	if limit <= 0 {
		return port.LedgerPage{}, port.ErrNotFound.Messagef("listar lançamentos: limite deve ser positivo")
	}

	// Busca uma linha além do limite para saber se existe página seguinte sem
	// precisar de uma contagem adicional.
	const query = `
		SELECT ` + ledgerColumns + `
		FROM wallet_ledger_entries
		WHERE wallet_id = $1
		  AND ($2::timestamptz IS NULL OR (created_at, id) > ($2, $3))
		ORDER BY created_at, id
		LIMIT $4`

	var (
		cursorCreatedAt *time.Time
		cursorID        *string
	)
	if after != nil {
		createdAt := after.CreatedAt
		id := after.ID.String()
		cursorCreatedAt, cursorID = &createdAt, &id
	}

	rows, err := r.db.Query(ctx, query, walletID.String(), cursorCreatedAt, cursorID, limit+1)
	if err != nil {
		return port.LedgerPage{}, translate("listar lançamentos", err)
	}
	defer rows.Close()

	entries := make([]ledger.Entry, 0, limit)
	for rows.Next() {
		entry, err := scanLedgerEntry(rows)
		if err != nil {
			return port.LedgerPage{}, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return port.LedgerPage{}, translate("listar lançamentos", err)
	}

	page := port.LedgerPage{Entries: entries}
	if len(entries) > limit {
		last := entries[limit-1]
		page.Entries = entries[:limit]
		page.Next = &port.LedgerCursor{CreatedAt: last.CreatedAt(), ID: last.ID()}
	}
	return page, nil
}

// SumByWallet reconstrói o saldo a partir dos lançamentos.
//
// A soma acontece no banco, em uma visão consistente, e devolve também a
// quantidade de lançamentos considerados, que a reconciliação reporta.
func (r *LedgerRepository) SumByWallet(ctx context.Context, walletID shared.ID) (money.Money, int, error) {
	var (
		total        int64
		entries      int
		currencyCode *string
	)

	err := r.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount_minor ELSE -amount_minor END), 0),
			COUNT(*),
			MIN(currency)
		FROM wallet_ledger_entries
		WHERE wallet_id = $1`, walletID.String()).Scan(&total, &entries, &currencyCode)
	if err != nil {
		return money.Money{}, 0, translate("somar lançamentos", err)
	}

	// Sem lançamentos não há moeda a inferir; o chamador usa a da carteira.
	if currencyCode == nil {
		return money.Money{}, 0, nil
	}

	currency, err := money.ParseCurrency(*currencyCode)
	if err != nil {
		return money.Money{}, 0, err
	}
	return money.FromMinorUnits(total, currency), entries, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanLedgerEntry(row scannable) (ledger.Entry, error) {
	var (
		id            string
		walletID      string
		transactionID string
		direction     string
		amountMinor   int64
		currencyCode  string
		balanceBefore int64
		balanceAfter  int64
		createdAt     time.Time
	)

	if err := row.Scan(&id, &walletID, &transactionID, &direction, &amountMinor, &currencyCode,
		&balanceBefore, &balanceAfter, &createdAt); err != nil {
		return ledger.Entry{}, translate("ler lançamento", err)
	}

	entryID, err := shared.ParseID(id)
	if err != nil {
		return ledger.Entry{}, err
	}
	wallet, err := shared.ParseID(walletID)
	if err != nil {
		return ledger.Entry{}, err
	}
	transaction, err := shared.ParseID(transactionID)
	if err != nil {
		return ledger.Entry{}, err
	}
	parsedDirection, err := ledger.ParseDirection(direction)
	if err != nil {
		return ledger.Entry{}, err
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return ledger.Entry{}, err
	}

	return ledger.NewEntry(
		entryID, wallet, transaction, parsedDirection,
		money.FromMinorUnits(amountMinor, currency),
		money.FromMinorUnits(balanceBefore, currency),
		money.FromMinorUnits(balanceAfter, currency),
		createdAt,
	)
}
