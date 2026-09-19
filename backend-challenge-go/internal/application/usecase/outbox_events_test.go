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
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
)

type captureOutbox struct {
	records []port.OutboxRecord
	err     error
}

func (c *captureOutbox) Append(_ context.Context, record port.OutboxRecord) error {
	if c.err != nil {
		return c.err
	}
	c.records = append(c.records, record)
	return nil
}

func (c *captureOutbox) Claim(context.Context, string, int, time.Duration, time.Time) ([]port.OutboxRecord, error) {
	return nil, nil
}
func (c *captureOutbox) MarkPublished(context.Context, shared.ID, time.Time) error { return nil }
func (c *captureOutbox) ReleaseWithBackoff(context.Context, shared.ID, int, time.Time, time.Time) error {
	return nil
}

type outboxOnlyRepos struct {
	outbox port.OutboxRepository
}

func (r outboxOnlyRepos) Wallets() port.WalletRepository           { return noopWallets{} }
func (r outboxOnlyRepos) Transactions() port.TransactionRepository { return noopTransactions{} }
func (r outboxOnlyRepos) Ledger() port.LedgerRepository            { return noopLedger{} }
func (r outboxOnlyRepos) Inbox() port.InboxRepository              { return noopInbox{} }
func (r outboxOnlyRepos) Outbox() port.OutboxRepository            { return r.outbox }

type seqIDs struct {
	next int
	fail int
}

func (g *seqIDs) NewID() (shared.ID, error) {
	g.next++
	if g.fail > 0 && g.next >= g.fail {
		return shared.ID{}, errors.New("id fail")
	}
	return shared.NewID()
}

func TestCorrelationOrNewUsesContextOrGenerates(t *testing.T) {
	fromCtx := correlationOrNew(correlation.WithID(context.Background(), "corr-from-edge"))
	if fromCtx != "corr-from-edge" {
		t.Errorf("correlation = %q, esperado corr-from-edge", fromCtx)
	}
	generated := correlationOrNew(context.Background())
	if generated == "" {
		t.Error("deveria gerar correlationId")
	}
}

func TestAppendOutboxPropagatesIDFailure(t *testing.T) {
	outbox := &captureOutbox{}
	ids := &seqIDs{fail: 1}
	tx := internalTransaction(t, "BET", "")

	err := appendTransactionEvent(context.Background(), outboxOnlyRepos{outbox: outbox}, ids,
		tx, EventWagerTransactionProcessed, aggregateTransaction, tx.ID(), internalNow)
	if err == nil {
		t.Fatal("esperava falha ao gerar eventId")
	}
}

func TestAppendOutboxPropagatesAppendFailure(t *testing.T) {
	outbox := &captureOutbox{err: errors.New("outbox down")}
	ids := &seqIDs{}
	tx := internalTransaction(t, "BET", "")

	err := appendRejectedEvent(context.Background(), outboxOnlyRepos{outbox: outbox}, ids, tx, internalNow)
	if err == nil {
		t.Fatal("esperava falha ao gravar outbox")
	}
}

func TestAppendBalanceChangedPropagatesAppendFailure(t *testing.T) {
	outbox := &captureOutbox{err: errors.New("outbox down")}
	ids := &seqIDs{}
	tx := internalTransaction(t, "BET", "")
	amount := internalMoney(t, "10.00")
	currency, err := money.ParseCurrency("BRL")
	if err != nil {
		t.Fatal(err)
	}
	zero := money.Zero(currency)

	err = appendBalanceChanged(context.Background(), outboxOnlyRepos{outbox: outbox}, ids, tx, &settlement{
		direction: ledger.Debit,
		movement: wallet.Movement{
			Amount: amount, BalanceBefore: amount, BalanceAfter: zero, Version: 2,
		},
	}, internalNow)
	if err == nil {
		t.Fatal("esperava falha ao gravar BalanceChanged")
	}
}

func TestAppendProcessedEventsWritesBalanceWhenApplied(t *testing.T) {
	outbox := &captureOutbox{}
	ids := &seqIDs{}
	tx := internalTransaction(t, "BET", "")
	amount := internalMoney(t, "10.00")
	currency, err := money.ParseCurrency("BRL")
	if err != nil {
		t.Fatal(err)
	}
	zero := money.Zero(currency)
	ctx := correlation.WithID(context.Background(), "corr-process")

	err = appendProcessedEvents(ctx, outboxOnlyRepos{outbox: outbox}, ids, tx, &settlement{
		direction: ledger.Debit,
		movement: wallet.Movement{
			Amount: amount, BalanceBefore: amount, BalanceAfter: zero, Version: 2,
		},
	}, internalNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox.records) != 2 {
		t.Fatalf("outbox = %d, esperados 2", len(outbox.records))
	}
	if outbox.records[0].CorrelationID != "corr-process" {
		t.Errorf("correlation = %s", outbox.records[0].CorrelationID)
	}
}
