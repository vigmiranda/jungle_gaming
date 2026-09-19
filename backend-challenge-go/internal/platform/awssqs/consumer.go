package awssqs

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
)

// MessageHandler processa o corpo de uma mensagem da fila de wagering.
type MessageHandler interface {
	Handle(ctx context.Context, raw []byte) (usecase.TransactionResult, error)
}

// Consumer faz long-poll da fila FIFO, processa com inbox e remove só após
// commit durável (ADR-008, ADR-016).
type Consumer struct {
	client  *sqs.Client
	handler MessageHandler
	log     *slog.Logger
	cfg     config.SQS
	cancel  context.CancelFunc
	done    sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// NewConsumer monta o worker. Com ConsumerEnabled=false, o lifecycle não inicia
// o loop — útil em testes de composição e em processos que só servem HTTP.
func NewConsumer(
	client *sqs.Client,
	handler *usecase.HandleWagerMessage,
	log *slog.Logger,
	cfg config.Config,
) *Consumer {
	return &Consumer{
		client:  client,
		handler: handler,
		log:     log,
		cfg:     cfg.SQS,
	}
}

// Register liga o consumidor ao ciclo de vida do Fx.
func RegisterConsumer(lc fx.Lifecycle, consumer *Consumer) {
	lc.Append(fx.Hook{
		OnStart: consumer.Start,
		OnStop:  consumer.Stop,
	})
}

// Start inicia o long-poll em background.
func (c *Consumer) Start(context.Context) error {
	if !c.cfg.ConsumerEnabled {
		c.log.Info("sqs_consumer_disabled")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancel = cancel
	c.running = true
	c.mu.Unlock()

	c.done.Add(1)
	go func() {
		defer c.done.Done()
		c.loop(ctx)
	}()

	c.log.Info("sqs_consumer_started",
		slog.String("queue", c.cfg.WagerQueueURL),
		slog.String("consumer", c.cfg.ConsumerName),
	)
	return nil
}

// Stop interrompe novas buscas e aguarda o lote em andamento.
func (c *Consumer) Stop(ctx context.Context) error {
	c.mu.Lock()
	running := c.running
	cancel := c.cancel
	c.mu.Unlock()

	if !running {
		return nil
	}

	c.log.Info("sqs_consumer_stopping")
	cancel()

	finished := make(chan struct{})
	go func() {
		c.done.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		c.log.Info("sqs_consumer_stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Consumer) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.poll(ctx); err != nil && ctx.Err() == nil {
			c.log.Error("sqs_poll_failed", slog.String("error", err.Error()))
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (c *Consumer) poll(ctx context.Context) error {
	output, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(c.cfg.WagerQueueURL),
		MaxNumberOfMessages: c.cfg.MaxMessages,
		WaitTimeSeconds:     int32(c.cfg.WaitTime.Seconds()),
		VisibilityTimeout:   int32(c.cfg.VisibilityTimeout.Seconds()),
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameApproximateReceiveCount,
			types.MessageSystemAttributeNameMessageGroupId,
			types.MessageSystemAttributeNameMessageDeduplicationId,
		},
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	for _, message := range output.Messages {
		c.handleOne(ctx, message)
	}
	return nil
}

func (c *Consumer) handleOne(ctx context.Context, message types.Message) {
	body := aws.ToString(message.Body)
	receipt := aws.ToString(message.ReceiptHandle)
	receiveCount := approximateReceiveCount(message)

	msgCtx := correlation.WithID(ctx, correlation.NewID())
	c.log.InfoContext(msgCtx, "sqs_message_received",
		slog.String("sqsMessageId", aws.ToString(message.MessageId)),
		slog.Int("receiveCount", receiveCount),
		slog.String("correlationId", correlation.FromContext(msgCtx)),
	)

	_, err := c.handler.Handle(msgCtx, []byte(body))
	if err == nil {
		if delErr := c.delete(msgCtx, receipt); delErr != nil {
			c.log.ErrorContext(msgCtx, "sqs_delete_failed", slog.String("error", delErr.Error()))
		}
		return
	}

	if isPoison(err) {
		c.log.WarnContext(msgCtx, "sqs_poison_message", slog.String("error", err.Error()))
		if moveErr := c.moveToDLQ(msgCtx, body, message); moveErr != nil {
			c.log.ErrorContext(msgCtx, "sqs_dlq_failed", slog.String("error", moveErr.Error()))
			return
		}
		if delErr := c.delete(msgCtx, receipt); delErr != nil {
			c.log.ErrorContext(msgCtx, "sqs_delete_failed", slog.String("error", delErr.Error()))
		}
		return
	}

	// Falha transitória: não delete; a visibility timeout devolve a mensagem.
	c.log.ErrorContext(msgCtx, "sqs_processing_failed",
		slog.String("error", err.Error()),
		slog.Int("receiveCount", receiveCount),
	)
}

func (c *Consumer) delete(ctx context.Context, receiptHandle string) error {
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.cfg.WagerQueueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	return err
}

func (c *Consumer) moveToDLQ(ctx context.Context, body string, message types.Message) error {
	groupID := message.Attributes[string(types.MessageSystemAttributeNameMessageGroupId)]
	if groupID == "" {
		groupID = "poison"
	}
	dedupeID := message.Attributes[string(types.MessageSystemAttributeNameMessageDeduplicationId)]
	if dedupeID == "" {
		dedupeID = aws.ToString(message.MessageId) + "-dlq"
	}

	_, err := c.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(c.cfg.WagerDLQURL),
		MessageBody:            aws.String(body),
		MessageGroupId:         aws.String(groupID),
		MessageDeduplicationId: aws.String(dedupeID),
	})
	return err
}

func approximateReceiveCount(message types.Message) int {
	raw := message.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]
	if raw == "" {
		return 1
	}
	count, err := strconv.Atoi(raw)
	if err != nil {
		return 1
	}
	return count
}

func isPoison(err error) bool {
	return errors.Is(err, usecase.ErrInvalidEnvelope) ||
		errors.Is(err, usecase.ErrUnauthorizedProvider) ||
		errors.Is(err, usecase.ErrInboxPayloadMismatch)
}
