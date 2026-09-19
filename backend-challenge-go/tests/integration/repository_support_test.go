//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
	"github.com/vigmi/backend-challenge-go/internal/platform/postgres/repository"
)

func newUnitOfWork() *repository.UnitOfWork {
	return repository.NewUnitOfWork(pool)
}

func newID(t *testing.T) shared.ID {
	t.Helper()
	id, err := shared.NewID()
	if err != nil {
		t.Fatalf("não foi possível gerar identificador: %v", err)
	}
	return id
}

func brl(t *testing.T, amount string) money.Money {
	t.Helper()
	parsed, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("Parse(%q) devolveu erro: %v", amount, err)
	}
	return parsed
}

// openWallet cria e persiste uma carteira com o saldo informado.
func openWallet(t *testing.T, ctx context.Context, balance string) *wallet.Wallet {
	t.Helper()

	opened, err := wallet.Open(newID(t), newID(t), brl(t, balance), time.Now().UTC())
	if err != nil {
		t.Fatalf("Open devolveu erro: %v", err)
	}

	err = newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		return repositories.Wallets().Create(ctx, opened)
	})
	if err != nil {
		t.Fatalf("não foi possível persistir a carteira: %v", err)
	}
	return opened
}

// newBet monta uma aposta externa pronta para ser persistida.
func newBet(t *testing.T, target *wallet.Wallet, amount string) *wagering.Transaction {
	t.Helper()

	externalID := "transaction-" + newID(t).String()
	created, err := wagering.NewExternal(wagering.ExternalParams{
		ID:             newID(t),
		Kind:           wagering.Bet,
		WalletID:       target.ID(),
		PlayerID:       target.PlayerID(),
		Amount:         brl(t, amount),
		ProviderID:     "provider-a",
		ExternalID:     externalID,
		IdempotencyKey: "provider-a:" + externalID,
		PayloadHash:    "3f786850e387550fdab836ed7e6dc881de23001b",
		RoundID:        "round-987",
		GameID:         "fortune-chimp",
		CreatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("NewExternal devolveu erro: %v", err)
	}
	return created
}

// applyBet reproduz a sequência que o caso de uso executará na etapa 5: lock da
// carteira, débito, persistência da transação, do lançamento e do saldo, tudo
// na mesma unidade de trabalho.
func applyBet(ctx context.Context, walletID shared.ID, transaction *wagering.Transaction) error {
	return newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		locked, err := repositories.Wallets().LockByID(ctx, walletID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		movement, err := locked.Debit(transaction.Amount(), now)
		if err != nil {
			return err
		}

		if err := transaction.MarkProcessed(wagering.Result{
			Balance:       movement.BalanceAfter,
			WalletVersion: movement.Version,
		}, now); err != nil {
			return err
		}
		if err := repositories.Transactions().Create(ctx, transaction); err != nil {
			return err
		}

		entryID, err := shared.NewID()
		if err != nil {
			return err
		}
		entry, err := ledger.NewEntry(entryID, locked.ID(), transaction.ID(), ledger.Debit,
			movement.Amount, movement.BalanceBefore, movement.BalanceAfter, now)
		if err != nil {
			return err
		}
		if err := repositories.Ledger().Append(ctx, entry); err != nil {
			return err
		}

		return repositories.Wallets().UpdateBalance(ctx, locked)
	})
}

func readWallet(t *testing.T, ctx context.Context, id shared.ID) *wallet.Wallet {
	t.Helper()

	found, err := newUnitOfWork().ReadOnly().Wallets().FindByID(ctx, id)
	if err != nil {
		t.Fatalf("não foi possível ler a carteira: %v", err)
	}
	return found
}

func countLedgerEntries(t *testing.T, ctx context.Context, walletID shared.ID) int {
	t.Helper()

	var total int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID.String()).Scan(&total); err != nil {
		t.Fatalf("não foi possível contar os lançamentos: %v", err)
	}
	return total
}
