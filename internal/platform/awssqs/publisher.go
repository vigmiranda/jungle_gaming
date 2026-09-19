package awssqs

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
)

// IntegrationSender publica o envelope na fila de integração.
type IntegrationSender interface {
	Send(ctx context.Context, queueURL string, body []byte) error
}

type sqsIntegrationSender struct {
	client *sqs.Client
}

func (s sqsIntegrationSender) Send(ctx context.Context, queueURL string, body []byte) error {
	_, err := s.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
	})
	return err
}

// Publisher reivindica lotes da outbox, publica na fila de integração e
// confirma ou reagenda com backoff (ADR-006).
type Publisher struct {
	uow      port.UnitOfWork
	clock    port.Clock
	sender   IntegrationSender
	log      *slog.Logger
	cfg      config.SQS
	recorder port.Recorder

	publisherID string
	cancel      context.CancelFunc
	done        sync.WaitGroup
	running     bool
	mu          sync.Mutex
}

// NewPublisher monta o worker. Com PublisherEnabled=false, o lifecycle não
// inicia o loop — útil em testes de composição.
func NewPublisher(
	client *sqs.Client,
	uow port.UnitOfWork,
	clock port.Clock,
	log *slog.Logger,
	cfg config.Config,
	recorder port.Recorder,
) *Publisher {
	return newPublisher(sqsIntegrationSender{client: client}, uow, clock, log, cfg.SQS, recorder)
}

// NewPublisherForTest monta o publisher com destino injetável (integração).
func NewPublisherForTest(
	sender IntegrationSender,
	uow port.UnitOfWork,
	clock port.Clock,
	log *slog.Logger,
	cfg config.SQS,
	recorder port.Recorder,
) *Publisher {
	return newPublisher(sender, uow, clock, log, cfg, recorder)
}

func newPublisher(
	sender IntegrationSender,
	uow port.UnitOfWork,
	clock port.Clock,
	log *slog.Logger,
	cfg config.SQS,
	recorder port.Recorder,
) *Publisher {
	return &Publisher{
		uow:         uow,
		clock:       clock,
		sender:      sender,
		log:         log,
		cfg:         cfg,
		recorder:    recorder,
		publisherID: resolvePublisherID(cfg.PublisherID),
	}
}

func resolvePublisherID(configured string) string {
	if configured != "" {
		return configured
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "publisher"
	}
	return host + "-" + uuid.NewString()
}

// RegisterPublisher liga o publisher ao ciclo de vida do Fx.
func RegisterPublisher(lc fx.Lifecycle, publisher *Publisher) {
	lc.Append(fx.Hook{
		OnStart: publisher.Start,
		OnStop:  publisher.Stop,
	})
}

// Start inicia o loop de publicação em background.
func (p *Publisher) Start(context.Context) error {
	if !p.cfg.PublisherEnabled {
		p.log.Info("outbox_publisher_disabled")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancel = cancel
	p.running = true
	p.mu.Unlock()

	p.done.Add(1)
	go func() {
		defer p.done.Done()
		p.loop(ctx)
	}()

	p.log.Info("outbox_publisher_started",
		slog.String("queue", p.cfg.IntegrationQueueURL),
		slog.String("publisherId", p.publisherID),
	)
	return nil
}

// Stop interrompe novos ticks e aguarda o lote em andamento.
func (p *Publisher) Stop(ctx context.Context) error {
	p.mu.Lock()
	running := p.running
	cancel := p.cancel
	p.mu.Unlock()

	if !running {
		return nil
	}

	p.log.Info("outbox_publisher_stopping")
	cancel()

	finished := make(chan struct{})
	go func() {
		p.done.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		p.log.Info("outbox_publisher_stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Publisher) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		published, err := p.tick(ctx)
		if err != nil && ctx.Err() == nil {
			p.log.Error("outbox_publisher_tick_failed", slog.String("error", err.Error()))
		}

		wait := p.cfg.PublisherPollInterval
		if published > 0 {
			wait = 0
		}
		if wait <= 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// Tick reivindica, publica e confirma um lote. Exposto para testes.
func (p *Publisher) Tick(ctx context.Context) (int, error) {
	return p.tick(ctx)
}

func (p *Publisher) tick(ctx context.Context) (int, error) {
	now := p.clock.Now().UTC()
	var claimed []port.OutboxRecord
	err := p.uow.Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		var claimErr error
		claimed, claimErr = repositories.Outbox().Claim(
			ctx, p.publisherID, p.cfg.PublisherBatchSize, p.cfg.PublisherLeaseTTL, now)
		return claimErr
	})
	if err != nil {
		return 0, err
	}
	if len(claimed) == 0 {
		return 0, nil
	}

	published := 0
	for _, record := range claimed {
		if ctx.Err() != nil {
			return published, ctx.Err()
		}
		if err := p.publishOne(ctx, record); err != nil {
			p.log.ErrorContext(ctx, "outbox_publish_failed",
				slog.String("eventId", record.ID.String()),
				slog.String("eventType", record.EventType),
				slog.String("error", err.Error()),
			)
			continue
		}
		published++
	}
	return published, nil
}

func (p *Publisher) publishOne(ctx context.Context, record port.OutboxRecord) error {
	msgCtx := correlation.WithID(ctx, record.CorrelationID)
	if !record.OccurredAt.IsZero() && p.recorder != nil {
		p.recorder.RecordOutboxLag(p.clock.Now().UTC().Sub(record.OccurredAt.UTC()).Seconds())
	}
	if err := p.sender.Send(msgCtx, p.cfg.IntegrationQueueURL, record.Payload); err != nil {
		return p.releaseAfterFailure(msgCtx, record, err)
	}

	now := p.clock.Now().UTC()
	markErr := p.uow.Execute(msgCtx, func(ctx context.Context, repositories port.Repositories) error {
		return repositories.Outbox().MarkPublished(ctx, record.ID, now)
	})
	if markErr != nil {
		// Publish já ocorreu: o lease expira e outro publisher republica com o
		// mesmo eventId. Consumidores externos precisam ser idempotentes.
		p.log.ErrorContext(msgCtx, "outbox_mark_published_failed",
			slog.String("eventId", record.ID.String()),
			slog.String("error", markErr.Error()),
		)
		return markErr
	}

	p.log.InfoContext(msgCtx, "outbox_event_published",
		slog.String("eventId", record.ID.String()),
		slog.String("eventType", record.EventType),
		slog.String("correlationId", record.CorrelationID),
	)
	return nil
}

func (p *Publisher) releaseAfterFailure(ctx context.Context, record port.OutboxRecord, publishErr error) error {
	now := p.clock.Now().UTC()
	attempts := record.Attempts + 1
	next := now.Add(backoffDuration(attempts, p.cfg.PublisherBackoffBase, p.cfg.PublisherBackoffMax))

	if p.recorder != nil {
		p.recorder.RecordRetry("outbox")
	}

	releaseErr := p.uow.Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		return repositories.Outbox().ReleaseWithBackoff(ctx, record.ID, attempts, next, now)
	})
	if releaseErr != nil {
		return releaseErr
	}
	return publishErr
}

func backoffDuration(attempts int, base, max time.Duration) time.Duration {
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
	if delay > max {
		return max
	}
	return delay
}

// PublisherID expõe o identificador usado no lease (testes).
func (p *Publisher) PublisherID() string { return p.publisherID }
