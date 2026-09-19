package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// PendingReferencePolicy calibra o worker de referências (ADR-009).
type PendingReferencePolicy struct {
	MaxAttempts int
	TTL         time.Duration
	BackoffBase time.Duration
	BackoffMax  time.Duration
	BatchSize   int
}

// ResolvePendingReferences retoma PENDING_REFERENCE com backoff e expiração.
type ResolvePendingReferences struct {
	unitOfWork port.UnitOfWork
	clock      port.Clock
	process    *ProcessWagerTransaction
	policy     PendingReferencePolicy
	recorder   port.Recorder
}

// NewResolvePendingReferences monta o caso de uso do worker.
func NewResolvePendingReferences(
	unitOfWork port.UnitOfWork,
	clock port.Clock,
	process *ProcessWagerTransaction,
	policy PendingReferencePolicy,
	recorder port.Recorder,
) *ResolvePendingReferences {
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 10
	}
	if policy.TTL <= 0 {
		policy.TTL = 5 * time.Minute
	}
	if policy.BackoffBase <= 0 {
		policy.BackoffBase = time.Second
	}
	if policy.BackoffMax <= 0 {
		policy.BackoffMax = 30 * time.Second
	}
	if policy.BatchSize < 1 {
		policy.BatchSize = 10
	}
	return &ResolvePendingReferences{
		unitOfWork: unitOfWork,
		clock:      clock,
		process:    process,
		policy:     policy,
		recorder:   recorder,
	}
}

// Tick processa até BatchSize pendências elegíveis.
func (uc *ResolvePendingReferences) Tick(ctx context.Context) (int, error) {
	processed := 0
	for processed < uc.policy.BatchSize {
		var progressed bool
		err := uc.unitOfWork.Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
			now := uc.clock.Now().UTC()
			claimed, err := repositories.Transactions().ClaimPendingReferences(ctx, 1, now)
			if err != nil {
				return err
			}
			if len(claimed) == 0 {
				return nil
			}
			progressed = true
			result, err := uc.resume(ctx, repositories, claimed[0], now)
			if err != nil {
				return err
			}
			uc.observePending(result)
			return nil
		})
		if err != nil {
			return processed, err
		}
		if !progressed {
			return processed, nil
		}
		processed++
	}
	return processed, nil
}

func (uc *ResolvePendingReferences) observePending(result TransactionResult) {
	if uc.recorder == nil || result.Status == "" {
		return
	}
	if result.Status == wagering.PendingReference {
		uc.recorder.RecordRetry("pending_reference")
		return
	}
	uc.recorder.RecordTransaction("pending_reference", string(result.Status), result.IdempotentReplay)
}

func (uc *ResolvePendingReferences) resume(
	ctx context.Context,
	repositories port.Repositories,
	pending *wagering.Transaction,
	now time.Time,
) (TransactionResult, error) {
	if pending.Status() != wagering.PendingReference {
		return TransactionResult{}, nil
	}

	target, err := repositories.Wallets().LockByID(ctx, pending.WalletID())
	if err != nil {
		return TransactionResult{}, err
	}

	reference, err := repositories.Transactions().FindByExternalID(
		ctx, pending.ProviderID(), pending.ReferenceExternalID())
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return uc.waitOrExpire(ctx, repositories, pending, now)
		}
		return TransactionResult{}, err
	}

	if !reference.Status().IsTerminal() {
		return uc.waitOrExpire(ctx, repositories, pending, now)
	}

	parsed := requestFromPending(pending)
	if code, ok := reversalRejection(reference, parsed); ok {
		return uc.process.rejectExisting(ctx, repositories, pending, code, now)
	}

	reverted, err := repositories.Transactions().HasSuccessfulReversal(ctx, reference.ID())
	if err != nil {
		return TransactionResult{}, err
	}
	if reverted {
		return uc.process.rejectExisting(ctx, repositories, pending, wagering.FailureDuplicateReversal, now)
	}

	if err := pending.ResolveReference(reference.ID(), now); err != nil {
		return TransactionResult{}, err
	}

	direction := ledger.Credit
	if reference.Kind() != wagering.Bet {
		direction = ledger.Debit
	}
	return uc.moveExisting(ctx, repositories, target, pending, direction, pending.Amount(), now)
}

func (uc *ResolvePendingReferences) waitOrExpire(
	ctx context.Context,
	repositories port.Repositories,
	pending *wagering.Transaction,
	now time.Time,
) (TransactionResult, error) {
	if uc.exhausted(pending, now) {
		return uc.process.rejectExisting(ctx, repositories, pending, wagering.FailureReferenceNotFound, now)
	}

	attempts := pending.AttemptCount() + 1
	next := now.Add(referenceBackoff(attempts, uc.policy.BackoffBase, uc.policy.BackoffMax))
	if err := pending.ScheduleReferenceRetry(attempts, next, now); err != nil {
		return TransactionResult{}, err
	}
	if err := repositories.Transactions().Update(ctx, pending); err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{
		TransactionID: pending.ID(),
		Status:        pending.Status(),
	}, nil
}

func (uc *ResolvePendingReferences) exhausted(pending *wagering.Transaction, now time.Time) bool {
	if pending.AttemptCount()+1 >= uc.policy.MaxAttempts {
		return true
	}
	return !pending.CreatedAt().Add(uc.policy.TTL).After(now)
}

func (uc *ResolvePendingReferences) moveExisting(
	ctx context.Context,
	repositories port.Repositories,
	target *wallet.Wallet,
	transaction *wagering.Transaction,
	direction ledger.Direction,
	amount money.Money,
	now time.Time,
) (TransactionResult, error) {
	var (
		movement wallet.Movement
		err      error
	)
	if direction == ledger.Credit {
		movement, err = target.Credit(amount, now)
	} else {
		movement, err = target.Debit(amount, now)
	}
	if err != nil {
		if errors.Is(err, wallet.ErrInsufficientFunds) {
			return uc.process.rejectExisting(ctx, repositories, transaction, insufficientFundsCode(transaction.Kind()), now)
		}
		return TransactionResult{}, err
	}

	return uc.process.settleExisting(ctx, repositories, target, transaction, &settlement{
		direction: direction,
		movement:  movement,
	}, now)
}

func requestFromPending(pending *wagering.Transaction) request {
	return request{
		kind:     pending.Kind(),
		walletID: pending.WalletID(),
		playerID: pending.PlayerID(),
		amount:   pending.Amount(),
		command: ProcessTransactionCommand{
			ProviderID:                     pending.ProviderID(),
			ExternalTransactionID:          pending.ExternalID(),
			ReferenceExternalTransactionID: pending.ReferenceExternalID(),
			RoundID:                        pending.RoundID(),
			GameID:                         pending.GameID(),
			PlayerID:                       pending.PlayerID().String(),
			WalletID:                       pending.WalletID().String(),
		},
	}
}

func referenceBackoff(attempts int, base, max time.Duration) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := base
	for i := 1; i < attempts; i++ {
		if delay >= max/2 {
			return max
		}
		delay *= 2
	}
	return delay
}
