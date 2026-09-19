//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/platform/awssqs"
	"github.com/vigmi/backend-challenge-go/internal/platform/clock"
	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
	"github.com/vigmi/backend-challenge-go/internal/platform/postgres/repository"
)

type fixedClock struct {
	now time.Time
}

func (c *fixedClock) Now() time.Time { return c.now.UTC() }

type recordingIntegrationSender struct {
	mu     sync.Mutex
	bodies [][]byte
	block  chan struct{}
}

func (s *recordingIntegrationSender) Send(ctx context.Context, _ string, body []byte) error {
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	return nil
}

func (s *recordingIntegrationSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func TestOutboxWrittenInSameCommitAsOpenWallet(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	open := usecase.NewOpenWallet(uow, clock.System{}, clock.UUIDGenerator{})
	corr := correlation.NewID()
	ctx = correlation.WithID(ctx, corr)

	if _, err := open.Execute(ctx, usecase.OpenWalletCommand{
		PlayerID:       newID(t).String(),
		InitialBalance: usecase.MoneyInput{Amount: "50.00", Currency: "BRL"},
	}); err != nil {
		t.Fatalf("OpenWallet: %v", err)
	}

	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE correlation_id = $1 AND published_at IS NULL`, corr).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("outbox pendente = %d, esperados 2 (Processed + BalanceChanged)", count)
	}
}

func TestOutboxClaimIsExclusiveBetweenPublishers(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := insertPendingOutbox(ctx, uow, now); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	first, err := claimBatch(ctx, uow, "publisher-a", 2, 30*time.Second, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("primeiro claim = %d, esperado 2", len(first))
	}

	second, err := claimBatch(ctx, uow, "publisher-b", 2, 30*time.Second, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 {
		t.Fatalf("segundo claim = %d, esperado 1", len(second))
	}

	for _, a := range first {
		for _, b := range second {
			if a.ID.Equal(b.ID) {
				t.Fatalf("evento %s reivindicado pelos dois publishers", a.ID)
			}
		}
	}
}

func TestOutboxExpiredLeaseIsReclaimedWithStableEventID(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	now := time.Now().UTC()
	if err := insertPendingOutbox(ctx, uow, now); err != nil {
		t.Fatal(err)
	}

	claimed, err := claimBatch(ctx, uow, "publisher-dead", 1, time.Second, now)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim inicial: %v / %d", err, len(claimed))
	}
	eventID := claimed[0].ID.String()
	originalPayload := string(claimed[0].Payload)

	// Lease ainda válido: ninguém mais pega.
	stillLocked, err := claimBatch(ctx, uow, "publisher-alive", 1, 30*time.Second, now.Add(500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(stillLocked) != 0 {
		t.Fatal("lease ativo não deveria ser reclaimed")
	}

	// Após expirar, outro publisher assume e republica o mesmo eventId.
	reclaimed, err := claimBatch(ctx, uow, "publisher-alive", 1, 30*time.Second, now.Add(2*time.Second))
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("reclaim: %v / %d", err, len(reclaimed))
	}
	if reclaimed[0].ID.String() != eventID {
		t.Errorf("id = %s, esperado %s", reclaimed[0].ID, eventID)
	}
	if string(reclaimed[0].Payload) != originalPayload {
		t.Error("payload/eventId deveriam ser estáveis na republicação")
	}

	var envelope struct {
		EventID string `json:"eventId"`
	}
	if err := json.Unmarshal(reclaimed[0].Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.EventID != eventID {
		t.Errorf("eventId no envelope = %s, esperado %s", envelope.EventID, eventID)
	}
}

func TestPublisherTickPublishesAndMarksAgainstPostgres(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	now := time.Now().UTC()
	if err := insertPendingOutbox(ctx, uow, now); err != nil {
		t.Fatal(err)
	}

	sender := &recordingIntegrationSender{}
	cfg := config.SQS{
		IntegrationQueueURL:   "http://local/integration",
		PublisherEnabled:      true,
		PublisherID:           "integration-publisher",
		PublisherBatchSize:    10,
		PublisherPollInterval: time.Hour,
		PublisherLeaseTTL:     30 * time.Second,
		PublisherBackoffBase:  time.Second,
		PublisherBackoffMax:   time.Minute,
	}

	// newPublisher é unexported; exercitamos via Tick através do construtor
	// de teste no pacote awssqs — usamos o fluxo Claim/Send/Mark aqui.
	publisher := awssqs.NewPublisherForTest(sender, uow, &fixedClock{now: now}, slog.Default(), cfg)

	published, err := publisher.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if published != 1 || sender.count() != 1 {
		t.Fatalf("published=%d envios=%d", published, sender.count())
	}

	var marked int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE published_at IS NOT NULL`).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if marked != 1 {
		t.Fatalf("marcados = %d, esperado 1", marked)
	}
}

func insertPendingOutbox(ctx context.Context, uow *repository.UnitOfWork, now time.Time) error {
	return uow.Execute(ctx, func(ctx context.Context, repos port.Repositories) error {
		id, err := shared.NewID()
		if err != nil {
			return err
		}
		aggregate, err := shared.NewID()
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"eventId":       id.String(),
			"eventType":     "WagerTransactionProcessed",
			"aggregateId":   aggregate.String(),
			"correlationId": "corr-outbox",
			"occurredAt":    now,
			"version":       1,
			"data":          map[string]string{"transactionId": aggregate.String()},
		})
		if err != nil {
			return err
		}
		return repos.Outbox().Append(ctx, port.OutboxRecord{
			ID:            id,
			AggregateType: "transaction",
			AggregateID:   aggregate,
			EventType:     "WagerTransactionProcessed",
			EventVersion:  1,
			Payload:       payload,
			CorrelationID: "corr-outbox",
			OccurredAt:    now,
			NextAttemptAt: now,
		})
	})
}

func claimBatch(
	ctx context.Context,
	uow *repository.UnitOfWork,
	publisherID string,
	limit int,
	leaseTTL time.Duration,
	now time.Time,
) ([]port.OutboxRecord, error) {
	var claimed []port.OutboxRecord
	err := uow.Execute(ctx, func(ctx context.Context, repos port.Repositories) error {
		var claimErr error
		claimed, claimErr = repos.Outbox().Claim(ctx, publisherID, limit, leaseTTL, now)
		return claimErr
	})
	return claimed, err
}
