package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

func TestResumeSkipsNonPendingStatus(t *testing.T) {
	uc := &ResolvePendingReferences{process: &ProcessWagerTransaction{}}
	tx := internalTransaction(t, wagering.Bet, "")
	if _, err := uc.resume(context.Background(), noopRepositories{}, tx, internalNow); err != nil {
		t.Fatal(err)
	}
}

func TestWaitOrExpirePropagatesScheduleFailure(t *testing.T) {
	uc := &ResolvePendingReferences{
		process: &ProcessWagerTransaction{ids: fixedIDs{}},
		policy: PendingReferencePolicy{
			MaxAttempts: 10, TTL: time.Hour,
			BackoffBase: time.Second, BackoffMax: time.Minute,
		},
	}
	tx := internalTransaction(t, wagering.Bet, "")
	if _, err := uc.waitOrExpire(context.Background(), noopRepositories{}, tx, internalNow); err == nil {
		t.Fatal("esperava falha ao agendar fora de PENDING_REFERENCE")
	}
}

func TestMoveExistingPropagatesOverflow(t *testing.T) {
	maxAmount, err := money.Parse("92233720368547758.07", "BRL")
	if err != nil {
		t.Fatal(err)
	}
	target, err := wallet.Open(
		internalID(t, "0192f291-27dd-7d3f-8071-5f8685deef37"),
		internalID(t, "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"),
		maxAmount, internalNow)
	if err != nil {
		t.Fatal(err)
	}

	pending := internalTransaction(t, wagering.Rollback, "bet-1")
	if err := pending.MarkPendingReference(internalNow); err != nil {
		t.Fatal(err)
	}

	uc := &ResolvePendingReferences{process: &ProcessWagerTransaction{ids: fixedIDs{}}}
	_, err = uc.moveExisting(
		context.Background(),
		noopRepositories{},
		target,
		pending,
		ledger.Credit,
		internalMoney(t, "0.01"),
		internalNow,
	)
	if !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("erro = %v, esperado overflow", err)
	}
}

func TestResumePropagatesZeroTimeOnResolveReference(t *testing.T) {
	pending := internalTransaction(t, wagering.Rollback, "bet-1")
	if err := pending.MarkPendingReference(internalNow); err != nil {
		t.Fatal(err)
	}
	reference := internalTransaction(t, wagering.Bet, "")
	if err := reference.MarkProcessed(wagering.Result{
		Balance: internalMoney(t, "975.00"), WalletVersion: 2,
	}, internalNow); err != nil {
		t.Fatal(err)
	}
	repos := &referenceReadyRepos{pending: pending, reference: reference, wallet: internalWallet(t)}

	uc := &ResolvePendingReferences{process: &ProcessWagerTransaction{ids: fixedIDs{}}}
	_, err := uc.resume(context.Background(), repos, pending, time.Time{})
	if err == nil {
		t.Fatal("esperava falha com instante zero")
	}
}

type referenceReadyRepos struct {
	pending   *wagering.Transaction
	reference *wagering.Transaction
	wallet    *wallet.Wallet
}

func (r *referenceReadyRepos) Wallets() port.WalletRepository {
	return &fixedWalletRepo{wallet: r.wallet}
}
func (r *referenceReadyRepos) Transactions() port.TransactionRepository {
	return &fixedTxRepo{pending: r.pending, reference: r.reference}
}
func (r *referenceReadyRepos) Ledger() port.LedgerRepository { return noopLedger{} }
func (r *referenceReadyRepos) Inbox() port.InboxRepository   { return noopInbox{} }
func (r *referenceReadyRepos) Outbox() port.OutboxRepository { return noopOutbox{} }

type fixedWalletRepo struct{ wallet *wallet.Wallet }

func (f *fixedWalletRepo) Create(context.Context, *wallet.Wallet) error { return nil }
func (f *fixedWalletRepo) LockByID(context.Context, shared.ID) (*wallet.Wallet, error) {
	return f.wallet, nil
}
func (f *fixedWalletRepo) FindByID(context.Context, shared.ID) (*wallet.Wallet, error) {
	return f.wallet, nil
}
func (f *fixedWalletRepo) UpdateBalance(context.Context, *wallet.Wallet) error { return nil }

type fixedTxRepo struct {
	pending   *wagering.Transaction
	reference *wagering.Transaction
}

func (f *fixedTxRepo) Create(context.Context, *wagering.Transaction) error { return nil }
func (f *fixedTxRepo) Update(context.Context, *wagering.Transaction) error { return nil }
func (f *fixedTxRepo) FindByID(context.Context, shared.ID) (*wagering.Transaction, error) {
	return f.pending, nil
}
func (f *fixedTxRepo) FindByIdempotencyKey(context.Context, string, string) (*wagering.Transaction, error) {
	return nil, port.ErrNotFound
}
func (f *fixedTxRepo) FindByExternalID(context.Context, string, string) (*wagering.Transaction, error) {
	return f.reference, nil
}
func (f *fixedTxRepo) HasSuccessfulReversal(context.Context, shared.ID) (bool, error) {
	return false, nil
}
func (f *fixedTxRepo) ClaimPendingReferences(context.Context, int, time.Time) ([]*wagering.Transaction, error) {
	return nil, nil
}
