//go:build integration

package integration

import (
	"testing"
)

func TestLedgerConstraints(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)
	transaction := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

	t.Run("lançamento consistente é aceito", func(t *testing.T) {
		if err := insertLedgerEntry(ctx, newLedgerRow(t, wallet, transaction.ID)); err != nil {
			t.Fatalf("lançamento válido deveria ser aceito: %v", err)
		}
	})

	// A mesma equação validada no domínio, imposta também no banco: um bug na
	// aplicação vira erro SQL em vez de saldo corrompido.
	t.Run("equação de saldo é imposta pelo banco", func(t *testing.T) {
		other := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		row := newLedgerRow(t, wallet, other.ID)
		row.BalanceAfter = row.BalanceBefore - row.AmountMinor + 1

		if err := insertLedgerEntry(ctx, row); err == nil {
			t.Fatal("esperava recusa do CHECK da equação de saldo")
		} else {
			assertConstraint(t, err, "wallet_ledger_entries_balance_equation")
		}
	})

	t.Run("crédito também obedece à equação", func(t *testing.T) {
		other := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		row := newLedgerRow(t, wallet, other.ID)
		row.Direction = "CREDIT"
		row.BalanceAfter = row.BalanceBefore - row.AmountMinor

		if err := insertLedgerEntry(ctx, row); err == nil {
			t.Fatal("esperava recusa: crédito reduzindo o saldo")
		} else {
			assertConstraint(t, err, "wallet_ledger_entries_balance_equation")
		}
	})

	t.Run("saldo posterior negativo é recusado", func(t *testing.T) {
		other := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		row := newLedgerRow(t, wallet, other.ID)
		row.BalanceBefore = 1000
		row.AmountMinor = 2500
		row.BalanceAfter = -1500

		if err := insertLedgerEntry(ctx, row); err == nil {
			t.Fatal("esperava recusa do CHECK de saldo não negativo")
		} else {
			assertConstraint(t, err, "wallet_ledger_entries_balances_non_negative")
		}
	})

	t.Run("valor não positivo é recusado", func(t *testing.T) {
		other := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		row := newLedgerRow(t, wallet, other.ID)
		row.AmountMinor = 0
		row.BalanceAfter = row.BalanceBefore

		if err := insertLedgerEntry(ctx, row); err == nil {
			t.Fatal("esperava recusa do CHECK de valor positivo")
		} else {
			assertConstraint(t, err, "wallet_ledger_entries_amount_positive")
		}
	})

	t.Run("direção desconhecida é recusada", func(t *testing.T) {
		other := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		row := newLedgerRow(t, wallet, other.ID)
		row.Direction = "TRANSFER"

		if err := insertLedgerEntry(ctx, row); err == nil {
			t.Fatal("esperava recusa do CHECK de direção")
		} else {
			assertConstraintOneOf(t, err,
				"wallet_ledger_entries_direction_valid",
				"wallet_ledger_entries_balance_equation")
		}
	})

	t.Run("moeda divergente da carteira é recusada", func(t *testing.T) {
		other := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		row := newLedgerRow(t, wallet, other.ID)
		row.Currency = "USD"

		if err := insertLedgerEntry(ctx, row); err == nil {
			t.Fatal("esperava recusa da chave estrangeira composta")
		} else {
			assertConstraint(t, err, "wallet_ledger_entries_wallet_currency_fk")
		}
	})
}

// Barreira final contra movimentação duplicada: mesmo que a aplicação tente
// aplicar a mesma transação duas vezes, o segundo lançamento não entra.
func TestLedgerAllowsOnlyOneEntryPerTransaction(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)
	transaction := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

	mustInsertLedgerEntry(t, ctx, newLedgerRow(t, wallet, transaction.ID))

	duplicate := newLedgerRow(t, wallet, transaction.ID)

	if err := insertLedgerEntry(ctx, duplicate); err == nil {
		t.Fatal("esperava recusa do segundo lançamento para a mesma transação")
	} else {
		assertConstraint(t, err, "wallet_ledger_entries_wallet_transaction_uq")
	}
}

// O ledger é append-only: correção financeira exige lançamento novo.
func TestLedgerIsAppendOnly(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)
	transaction := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))
	entry := mustInsertLedgerEntry(t, ctx, newLedgerRow(t, wallet, transaction.ID))

	t.Run("UPDATE é recusado", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`UPDATE wallet_ledger_entries SET amount_minor = amount_minor + 1 WHERE id = $1`, entry.ID)

		if err == nil {
			t.Fatal("esperava recusa do gatilho de imutabilidade")
		}
		assertErrorCode(t, err, "23001", "append-only")
	})

	t.Run("DELETE é recusado", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM wallet_ledger_entries WHERE id = $1`, entry.ID)

		if err == nil {
			t.Fatal("esperava recusa do gatilho de imutabilidade")
		}
		assertErrorCode(t, err, "23001", "append-only")
	})

	t.Run("o lançamento continua intacto", func(t *testing.T) {
		var amount int64
		if err := pool.QueryRow(ctx,
			`SELECT amount_minor FROM wallet_ledger_entries WHERE id = $1`, entry.ID).Scan(&amount); err != nil {
			t.Fatalf("não foi possível ler o lançamento: %v", err)
		}
		if amount != entry.AmountMinor {
			t.Errorf("amount_minor = %d, esperado %d", amount, entry.AmountMinor)
		}
	})
}

// A paginação do ledger usa cursor opaco sobre (created_at, id), então a
// ordenação precisa ser estável mesmo com lançamentos no mesmo instante.
func TestLedgerCursorOrderingIsStable(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)

	const entries = 5
	for index := 0; index < entries; index++ {
		transaction := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))
		mustInsertLedgerEntry(t, ctx, newLedgerRow(t, wallet, transaction.ID))
	}

	rows, err := pool.Query(ctx, `
		SELECT id, created_at
		FROM wallet_ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at, id
		LIMIT 2`, wallet.ID)
	if err != nil {
		t.Fatalf("não foi possível consultar a primeira página: %v", err)
	}
	defer rows.Close()

	type cursor struct {
		id        string
		createdAt any
	}
	var page []cursor
	for rows.Next() {
		var current cursor
		if err := rows.Scan(&current.id, &current.createdAt); err != nil {
			t.Fatalf("não foi possível ler a linha: %v", err)
		}
		page = append(page, current)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("erro ao percorrer a página: %v", err)
	}

	if len(page) != 2 {
		t.Fatalf("primeira página trouxe %d lançamentos, esperados 2", len(page))
	}

	last := page[len(page)-1]
	var remaining int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM wallet_ledger_entries
		WHERE wallet_id = $1 AND (created_at, id) > ($2, $3)`,
		wallet.ID, last.createdAt, last.id).Scan(&remaining); err != nil {
		t.Fatalf("não foi possível contar o restante: %v", err)
	}

	if remaining != entries-2 {
		t.Errorf("restaram %d lançamentos após o cursor, esperados %d", remaining, entries-2)
	}
}
