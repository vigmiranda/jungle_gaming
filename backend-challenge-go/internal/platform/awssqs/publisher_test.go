package awssqs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

type captureSender struct {
	bodies [][]byte
	err    error
}

func (s *captureSender) Send(_ context.Context, _ string, body []byte) error {
	if s.err != nil {
		return s.err
	}
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	return nil
}

type memoryClock struct {
	now time.Time
}

func (c *memoryClock) Now() time.Time { return c.now }

type memoryOutbox struct {
	mu      sync.Mutex
	records map[string]port.OutboxRecord
}

func newMemoryOutbox(records ...port.OutboxRecord) *memoryOutbox {
	store := &memoryOutbox{records: make(map[string]port.OutboxRecord)}
	for _, record := range records {
		store.records[record.ID.String()] = record
	}
	return store
}

func (o *memoryOutbox) Append(_ context.Context, record port.OutboxRecord) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records[record.ID.String()] = record
	return nil
}

func (o *memoryOutbox) Claim(
	_ context.Context,
	publisherID string,
	limit int,
	leaseTTL time.Duration,
	now time.Time,
) ([]port.OutboxRecord, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	var claimed []port.OutboxRecord
	for _, record := range o.records {
		if record.HasPublished {
			continue
		}
		if record.NextAttemptAt.After(now) {
			continue
		}
		if record.HasLease && record.LockedUntil.After(now) {
			continue
		}
		record.LockedBy = publisherID
		record.LockedUntil = now.Add(leaseTTL)
		record.HasLease = true
		o.records[record.ID.String()] = record
		claimed = append(claimed, record)
		if len(claimed) >= limit {
			break
		}
	}
	return claimed, nil
}

func (o *memoryOutbox) MarkPublished(_ context.Context, id shared.ID, now time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	record, ok := o.records[id.String()]
	if !ok {
		return port.ErrNotFound
	}
	record.PublishedAt = now
	record.HasPublished = true
	record.HasLease = false
	record.LockedBy = ""
	record.LockedUntil = time.Time{}
	o.records[id.String()] = record
	return nil
}

func (o *memoryOutbox) ReleaseWithBackoff(
	_ context.Context,
	id shared.ID,
	attempts int,
	nextAttemptAt, _ time.Time,
) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	record, ok := o.records[id.String()]
	if !ok {
		return port.ErrNotFound
	}
	record.Attempts = attempts
	record.NextAttemptAt = nextAttemptAt
	record.HasLease = false
	record.LockedBy = ""
	record.LockedUntil = time.Time{}
	o.records[id.String()] = record
	return nil
}

type memoryRepos struct {
	outbox *memoryOutbox
}

func (r memoryRepos) Wallets() port.WalletRepository           { return nil }
func (r memoryRepos) Transactions() port.TransactionRepository { return nil }
func (r memoryRepos) Ledger() port.LedgerRepository            { return nil }
func (r memoryRepos) Inbox() port.InboxRepository              { return nil }
func (r memoryRepos) Outbox() port.OutboxRepository            { return r.outbox }

type memoryUoW struct {
	outbox *memoryOutbox
}

func (u memoryUoW) Execute(ctx context.Context, fn func(context.Context, port.Repositories) error) error {
	return fn(ctx, memoryRepos{outbox: u.outbox})
}

func (u memoryUoW) ExecuteReadOnly(ctx context.Context, fn func(context.Context, port.Repositories) error) error {
	return fn(ctx, memoryRepos{outbox: u.outbox})
}

func testPublisherConfig() config.SQS {
	return config.SQS{
		IntegrationQueueURL:   "http://localhost/integration",
		PublisherEnabled:      true,
		PublisherID:           "publisher-a",
		PublisherBatchSize:    10,
		PublisherPollInterval: time.Hour,
		PublisherLeaseTTL:     5 * time.Second,
		PublisherBackoffBase:  time.Second,
		PublisherBackoffMax:   time.Minute,
	}
}

func sampleOutboxRecord(t *testing.T, eventID string) port.OutboxRecord {
	t.Helper()
	id, err := shared.ParseID(eventID)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := shared.ParseID("0192f2a0-0000-7000-8000-000000000099")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"eventId":       eventID,
		"eventType":     "WagerTransactionProcessed",
		"aggregateId":   aggregate.String(),
		"correlationId": "corr-1",
		"occurredAt":    time.Now().UTC(),
		"version":       1,
		"data":          map[string]string{"transactionId": aggregate.String()},
	})
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	return port.OutboxRecord{
		ID:            id,
		AggregateType: "transaction",
		AggregateID:   aggregate,
		EventType:     "WagerTransactionProcessed",
		EventVersion:  1,
		Payload:       payload,
		CorrelationID: "corr-1",
		OccurredAt:    now,
		NextAttemptAt: now,
	}
}

func TestPublisherPublishesAndMarks(t *testing.T) {
	record := sampleOutboxRecord(t, "0192f2a0-0000-7000-8000-000000000001")
	outbox := newMemoryOutbox(record)
	sender := &captureSender{}
	clock := &memoryClock{now: record.OccurredAt}
	publisher := newPublisher(sender, memoryUoW{outbox: outbox}, clock, slog.Default(), testPublisherConfig(), nil)

	published, err := publisher.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if published != 1 {
		t.Fatalf("published = %d, esperado 1", published)
	}
	if len(sender.bodies) != 1 {
		t.Fatalf("envios = %d, esperado 1", len(sender.bodies))
	}
	stored := outbox.records[record.ID.String()]
	if !stored.HasPublished {
		t.Fatal("evento deveria estar marcado como publicado")
	}
}

func TestPublisherReleasesWithBackoffOnSendFailure(t *testing.T) {
	record := sampleOutboxRecord(t, "0192f2a0-0000-7000-8000-000000000002")
	outbox := newMemoryOutbox(record)
	sender := &captureSender{err: errors.New("broker down")}
	clock := &memoryClock{now: record.OccurredAt}
	publisher := newPublisher(sender, memoryUoW{outbox: outbox}, clock, slog.Default(), testPublisherConfig(), nil)

	published, err := publisher.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if published != 0 {
		t.Fatalf("published = %d, esperado 0", published)
	}
	stored := outbox.records[record.ID.String()]
	if stored.HasPublished {
		t.Fatal("não deveria marcar publicado após falha")
	}
	if stored.Attempts != 1 {
		t.Errorf("attempts = %d, esperado 1", stored.Attempts)
	}
	if !stored.NextAttemptAt.After(clock.now) {
		t.Error("next_attempt_at deveria avançar com backoff")
	}
	if stored.HasLease {
		t.Error("lease deveria ser liberado após falha")
	}
}

func TestPublisherRepublishesSameEventIDAfterAbandonedLease(t *testing.T) {
	record := sampleOutboxRecord(t, "0192f2a0-0000-7000-8000-000000000003")
	outbox := newMemoryOutbox(record)
	clock := &memoryClock{now: record.OccurredAt}
	cfg := testPublisherConfig()

	firstSender := &captureSender{}
	first := newPublisher(firstSender, memoryUoW{outbox: outbox}, clock, slog.Default(), cfg, nil)
	// Simula kill entre publish e mark: claim + send, sem MarkPublished.
	claimed, err := outbox.Claim(context.Background(), first.PublisherID(), 1, cfg.PublisherLeaseTTL, clock.now)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim inicial: %v / %d", err, len(claimed))
	}
	if err := firstSender.Send(context.Background(), cfg.IntegrationQueueURL, claimed[0].Payload); err != nil {
		t.Fatal(err)
	}

	// Lease ainda válido: segundo publisher não pega.
	secondCfg := cfg
	secondCfg.PublisherID = "publisher-b"
	secondSender := &captureSender{}
	second := newPublisher(secondSender, memoryUoW{outbox: outbox}, clock, slog.Default(), secondCfg, nil)
	published, err := second.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if published != 0 || len(secondSender.bodies) != 0 {
		t.Fatal("lease ativo não deveria ser assumido")
	}

	// Lease expirado: republica com o mesmo eventId no payload.
	clock.now = clock.now.Add(cfg.PublisherLeaseTTL + time.Second)
	published, err = second.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if published != 1 {
		t.Fatalf("published após lease = %d, esperado 1", published)
	}
	if len(secondSender.bodies) != 1 {
		t.Fatal("esperava republicação")
	}

	var envelope struct {
		EventID string `json:"eventId"`
	}
	if err := json.Unmarshal(secondSender.bodies[0], &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.EventID != record.ID.String() {
		t.Errorf("eventId = %s, esperado %s (estável)", envelope.EventID, record.ID)
	}
}

func TestBackoffDurationCapsAtMax(t *testing.T) {
	got := backoffDuration(10, time.Second, 8*time.Second)
	if got != 8*time.Second {
		t.Errorf("backoff = %s, esperado 8s", got)
	}
	if backoffDuration(1, time.Second, time.Minute) != time.Second {
		t.Error("primeira tentativa deveria usar a base")
	}
}
