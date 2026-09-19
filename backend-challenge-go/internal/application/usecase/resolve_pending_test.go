package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

func TestResolvePendingAppliesReversalAfterBetArrives(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")

	pending, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-1", "ROLLBACK", "bet-late", "25.00"))
	if err != nil {
		t.Fatalf("pendência: %v", err)
	}
	if pending.Status != wagering.PendingReference {
		t.Fatalf("status = %s", pending.Status)
	}

	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-late", "25.00")); err != nil {
		t.Fatalf("aposta: %v", err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10,
		TTL:         time.Hour,
		BackoffBase: time.Second,
		BackoffMax:  time.Minute,
		BatchSize:   10,
	}, nil)

	processed, err := resolve.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, esperado 1", processed)
	}

	stored := f.unitOfWork.state.transactions[pending.TransactionID.String()]
	if stored.Status() != wagering.Processed {
		t.Errorf("status = %s, esperado PROCESSED", stored.Status())
	}
	if balance := f.unitOfWork.state.wallets[walletID].wallet.Balance().String(); balance != "100.00" {
		t.Errorf("saldo = %s, esperado 100.00 após rollback", balance)
	}
	if got := countOutboxType(f.unitOfWork.state.outbox, usecase.EventWagerTransactionProcessed); got < 2 {
		t.Errorf("Processed na outbox = %d, esperava ao menos abertura+resolução", got)
	}
}

func TestResolvePendingExpiresWithReferenceNotFound(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")

	pending, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-expire", "ROLLBACK", "bet-never", "10.00"))
	if err != nil {
		t.Fatalf("pendência: %v", err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 2,
		TTL:         time.Hour,
		BackoffBase: time.Millisecond,
		BackoffMax:  time.Millisecond,
		BatchSize:   10,
	}, nil)

	if _, err := resolve.Tick(context.Background()); err != nil {
		t.Fatalf("primeira tentativa: %v", err)
	}
	stored := f.unitOfWork.state.transactions[pending.TransactionID.String()]
	if stored.Status() != wagering.PendingReference || stored.AttemptCount() != 1 {
		t.Fatalf("após 1ª tentativa: status=%s attempts=%d", stored.Status(), stored.AttemptCount())
	}

	// Avança o relógio além do next_retry_at.
	next, _ := stored.NextRetryAt()
	f.clock.now = next

	if _, err := resolve.Tick(context.Background()); err != nil {
		t.Fatalf("segunda tentativa: %v", err)
	}
	stored = f.unitOfWork.state.transactions[pending.TransactionID.String()]
	if stored.Status() != wagering.Rejected {
		t.Fatalf("status = %s, esperado REJECTED", stored.Status())
	}
	if stored.FailureCode() != wagering.FailureReferenceNotFound {
		t.Errorf("failureCode = %s", stored.FailureCode())
	}
	if got := countOutboxType(f.unitOfWork.state.outbox, usecase.EventWagerTransactionRejected); got != 1 {
		t.Errorf("Rejected na outbox = %d", got)
	}
}

func TestResolvePendingExpiresByTTL(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")

	pending, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-ttl", "ROLLBACK", "bet-ttl", "10.00"))
	if err != nil {
		t.Fatalf("pendência: %v", err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 100,
		TTL:         time.Minute,
		BackoffBase: time.Second,
		BackoffMax:  time.Minute,
		BatchSize:   10,
	}, nil)

	f.clock.now = fixedNow.Add(2 * time.Minute)

	if _, err := resolve.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	stored := f.unitOfWork.state.transactions[pending.TransactionID.String()]
	if stored.Status() != wagering.Rejected || stored.FailureCode() != wagering.FailureReferenceNotFound {
		t.Fatalf("resultado = %s / %s", stored.Status(), stored.FailureCode())
	}
}

func TestResolvePendingRejectsIneligibleReference(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")

	// REFUND pendente da aposta; a "referência" chega como WIN (incompatível).
	pending, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "refund-1", "REFUND", "not-a-bet", "25.00"))
	if err != nil {
		t.Fatalf("pendência: %v", err)
	}

	win := betCommand(walletID, "not-a-bet", "25.00")
	win.Kind = "WIN"
	if _, err := f.process.Execute(context.Background(), win); err != nil {
		t.Fatalf("win: %v", err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10,
		TTL:         time.Hour,
		BatchSize:   10,
	}, nil)
	if _, err := resolve.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	stored := f.unitOfWork.state.transactions[pending.TransactionID.String()]
	if stored.FailureCode() != wagering.FailureReferenceNotProcessed {
		t.Errorf("failureCode = %s, esperado REFERENCE_NOT_PROCESSED", stored.FailureCode())
	}
}

func TestResolvePendingWaitsWhileReferenceStillPending(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")

	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "ref-pending", "REFUND", "missing-bet", "10.00")); err != nil {
		t.Fatal(err)
	}

	waiting, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-wait", "ROLLBACK", "ref-pending", "10.00"))
	if err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10,
		TTL:         time.Hour,
		BackoffBase: time.Second,
		BackoffMax:  time.Minute,
		BatchSize:   10,
	}, nil)
	if _, err := resolve.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	stored := f.unitOfWork.state.transactions[waiting.TransactionID.String()]
	if stored.Status() != wagering.PendingReference || stored.AttemptCount() < 1 {
		t.Fatalf("deveria continuar pendente com retry, status=%s attempts=%d", stored.Status(), stored.AttemptCount())
	}
}

func TestResolvePendingRejectsDuplicateReversal(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")

	pending, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-dup", "ROLLBACK", "bet-1", "25.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "25.00")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "refund-1", "REFUND", "bet-1", "25.00")); err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BatchSize: 10,
	}, nil)
	if _, err := resolve.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	stored := f.unitOfWork.state.transactions[pending.TransactionID.String()]
	if stored.FailureCode() != wagering.FailureDuplicateReversal {
		t.Errorf("failureCode = %s", stored.FailureCode())
	}
}

func TestResolvePendingRejectsWhenReversalExceedsBalance(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "10.00")

	pending, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-win", "ROLLBACK", "win-1", "25.00"))
	if err != nil {
		t.Fatal(err)
	}

	win := betCommand(walletID, "win-1", "25.00")
	win.Kind = "WIN"
	if _, err := f.process.Execute(context.Background(), win); err != nil {
		t.Fatal(err)
	}
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "drain", "20.00")); err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BatchSize: 10,
	}, nil)
	if _, err := resolve.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	stored := f.unitOfWork.state.transactions[pending.TransactionID.String()]
	if stored.FailureCode() != wagering.FailureReversalExceedsBalance {
		t.Errorf("failureCode = %s", stored.FailureCode())
	}
}

func TestResolvePendingPropagatesInfrastructureErrors(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-err", "ROLLBACK", "bet-x", "10.00")); err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BatchSize: 10,
	}, nil)
	f.unitOfWork.state.walletLockErr = errors.New("lock fail")
	if _, err := resolve.Tick(context.Background()); err == nil {
		t.Fatal("esperava erro de lock")
	}

	f.unitOfWork.state.walletLockErr = nil
	f.unitOfWork.state.findExternalErr = errors.New("lookup fail")
	if _, err := resolve.Tick(context.Background()); err == nil {
		t.Fatal("esperava erro de lookup")
	}

	f.unitOfWork.state.findExternalErr = nil
	f.unitOfWork.state.transactionErr = errors.New("update fail")
	if _, err := resolve.Tick(context.Background()); err == nil {
		t.Fatal("esperava erro ao reagendar")
	}
}

func TestResolvePendingPropagatesClaimError(t *testing.T) {
	f := newFixture()
	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BatchSize: 5,
	}, nil)
	f.unitOfWork.state.claimPendingErr = errors.New("claim fail")
	if _, err := resolve.Tick(context.Background()); err == nil {
		t.Fatal("esperava erro no claim")
	}
}

func TestResolvePendingTickWithEmptyQueue(t *testing.T) {
	f := newFixture()
	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BatchSize: 5,
	}, nil)
	processed, err := resolve.Tick(context.Background())
	if err != nil || processed != 0 {
		t.Fatalf("processed=%d err=%v", processed, err)
	}
}

func TestResolvePendingTickFillsBatch(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "r1", "ROLLBACK", "missing-1", "10.00")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "r2", "ROLLBACK", "missing-2", "10.00")); err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BackoffBase: time.Second, BackoffMax: time.Minute, BatchSize: 1,
	}, nil)
	n, err := resolve.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("batch size 1 deveria processar exatamente 1, got %d", n)
	}
}

func TestResolvePendingPropagatesUpdateFailureOnReject(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-rej", "ROLLBACK", "never", "10.00")); err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 1, TTL: time.Hour, BatchSize: 10,
	}, nil)
	f.unitOfWork.state.transactionErr = errors.New("update fail")
	if _, err := resolve.Tick(context.Background()); err == nil {
		t.Fatal("esperava erro ao rejeitar por expiração")
	}
}

func TestResolvePendingPropagatesReversalCheckError(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-check", "ROLLBACK", "bet-check", "25.00")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-check", "25.00")); err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BatchSize: 10,
	}, nil)
	f.unitOfWork.state.reversalCheckErr = errors.New("check fail")
	if _, err := resolve.Tick(context.Background()); err == nil {
		t.Fatal("esperava erro na checagem de reversão")
	}
}

func TestResolvePendingPropagatesUpdateFailureOnSettle(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-upd", "ROLLBACK", "bet-upd", "25.00")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-upd", "25.00")); err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BatchSize: 10,
	}, nil)
	f.unitOfWork.state.transactionErr = errors.New("update fail")
	if _, err := resolve.Tick(context.Background()); err == nil {
		t.Fatal("esperava erro ao confirmar PROCESSED")
	}
}

func TestResolvePendingPropagatesLedgerFailureOnSettle(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-led", "ROLLBACK", "bet-led", "25.00")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-led", "25.00")); err != nil {
		t.Fatal(err)
	}

	resolve := usecase.NewResolvePendingReferences(f.unitOfWork, f.clock, f.process, usecase.PendingReferencePolicy{
		MaxAttempts: 10, TTL: time.Hour, BatchSize: 10,
	}, nil)
	f.unitOfWork.state.ledgerAppendErr = errors.New("ledger fail")
	if _, err := resolve.Tick(context.Background()); err == nil {
		t.Fatal("esperava erro no ledger")
	}
}

func TestNewResolvePendingReferencesFromConfig(t *testing.T) {
	uc := usecase.NewResolvePendingReferencesFromConfig(nil, nil, nil, config.Config{
		PendingReference: config.PendingReference{
			MaxAttempts: 3,
			TTL:         2 * time.Minute,
			BackoffBase: time.Second,
			BackoffMax:  5 * time.Second,
			BatchSize:   4,
		},
	}, nil)
	if uc == nil {
		t.Fatal("nil")
	}
}
