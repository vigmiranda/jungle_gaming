//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/platform/clock"
	"github.com/vigmi/backend-challenge-go/internal/platform/postgres/repository"
)

func TestPendingReferenceResolvesWhenBetArrivesLater(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	ids := clock.UUIDGenerator{}
	sysClock := clock.System{}
	process := usecase.NewProcessWagerTransaction(uow, sysClock, ids)
	open := usecase.NewOpenWallet(uow, sysClock, ids)
	resolve := usecase.NewResolvePendingReferences(uow, sysClock, process, usecase.PendingReferencePolicy{
		MaxAttempts: 10,
		TTL:         time.Hour,
		BackoffBase: time.Millisecond,
		BackoffMax:  time.Second,
		BatchSize:   10,
	})

	opened, err := open.Execute(ctx, usecase.OpenWalletCommand{
		PlayerID:       newID(t).String(),
		InitialBalance: usecase.MoneyInput{Amount: "100.00", Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("OpenWallet: %v", err)
	}
	walletID := opened.Wallet.ID().String()
	playerID := opened.Wallet.PlayerID().String()

	pending, err := process.Execute(ctx, usecase.ProcessTransactionCommand{
		IdempotencyKey:                 "provider-a:rollback-early",
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "rollback-early",
		PlayerID:                       playerID,
		WalletID:                       walletID,
		RoundID:                        "round-1",
		GameID:                         "game-1",
		Kind:                           "ROLLBACK",
		ReferenceExternalTransactionID: "bet-late",
		Money:                          usecase.MoneyInput{Amount: "25.00", Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("rollback antecipado: %v", err)
	}
	if pending.Status != wagering.PendingReference {
		t.Fatalf("status = %s", pending.Status)
	}

	if _, err := process.Execute(ctx, usecase.ProcessTransactionCommand{
		IdempotencyKey:        "provider-a:bet-late",
		ProviderID:            "provider-a",
		ExternalTransactionID: "bet-late",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  "BET",
		Money:                 usecase.MoneyInput{Amount: "25.00", Currency: "BRL"},
	}); err != nil {
		t.Fatalf("bet: %v", err)
	}

	processed, err := resolve.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d", processed)
	}

	found, err := uow.ReadOnly().Transactions().FindByID(ctx, pending.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status() != wagering.Processed {
		t.Fatalf("status = %s", found.Status())
	}

	wallet, err := uow.ReadOnly().Wallets().FindByID(ctx, opened.Wallet.ID())
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Balance().String() != "100.00" {
		t.Fatalf("saldo = %s, esperado 100.00", wallet.Balance())
	}
}

func TestPendingReferenceExpiresAcrossWorkerRestarts(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	ids := clock.UUIDGenerator{}
	fixed := &fixedClock{now: time.Now().UTC()}
	process := usecase.NewProcessWagerTransaction(uow, fixed, ids)
	open := usecase.NewOpenWallet(uow, fixed, ids)

	opened, err := open.Execute(ctx, usecase.OpenWalletCommand{
		PlayerID:       newID(t).String(),
		InitialBalance: usecase.MoneyInput{Amount: "50.00", Currency: "BRL"},
	})
	if err != nil {
		t.Fatal(err)
	}

	pending, err := process.Execute(ctx, usecase.ProcessTransactionCommand{
		IdempotencyKey:                 "provider-a:rollback-restart",
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "rollback-restart",
		PlayerID:                       opened.Wallet.PlayerID().String(),
		WalletID:                       opened.Wallet.ID().String(),
		RoundID:                        "round-1",
		GameID:                         "game-1",
		Kind:                           "ROLLBACK",
		ReferenceExternalTransactionID: "missing-bet",
		Money:                          usecase.MoneyInput{Amount: "10.00", Currency: "BRL"},
	})
	if err != nil {
		t.Fatal(err)
	}

	policy := usecase.PendingReferencePolicy{
		MaxAttempts: 2,
		TTL:         time.Hour,
		BackoffBase: time.Second,
		BackoffMax:  time.Second,
		BatchSize:   5,
	}

	// Primeira instância agenda retry e "morre".
	first := usecase.NewResolvePendingReferences(uow, fixed, process, policy)
	if _, err := first.Tick(ctx); err != nil {
		t.Fatalf("primeiro worker: %v", err)
	}

	stored, err := uow.ReadOnly().Transactions().FindByID(ctx, pending.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status() != wagering.PendingReference || stored.AttemptCount() != 1 {
		t.Fatalf("após restart parcial: %s attempts=%d", stored.Status(), stored.AttemptCount())
	}
	next, ok := stored.NextRetryAt()
	if !ok {
		t.Fatal("next_retry_at ausente")
	}

	// Segunda instância retoma após o backoff.
	fixed.now = next.Add(time.Millisecond)
	second := usecase.NewResolvePendingReferences(uow, fixed, process, policy)
	if _, err := second.Tick(ctx); err != nil {
		t.Fatalf("segundo worker: %v", err)
	}

	stored, err = uow.ReadOnly().Transactions().FindByID(ctx, pending.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status() != wagering.Rejected || stored.FailureCode() != wagering.FailureReferenceNotFound {
		t.Fatalf("resultado = %s / %s", stored.Status(), stored.FailureCode())
	}
}
