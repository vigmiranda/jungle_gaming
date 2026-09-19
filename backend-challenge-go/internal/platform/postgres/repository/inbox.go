package repository

import (
	"context"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// InboxRepository persiste a deduplicação de entregas at-least-once.
type InboxRepository struct {
	db querier
}

// Record grava a entrega. Usa `ON CONFLICT DO NOTHING` para que a unicidade
// não aborte a transação: a reentrega precisa consultar o hash no mesmo UoW.
func (r *InboxRepository) Record(ctx context.Context, message port.InboxMessage) error {
	tag, err := r.db.Exec(ctx, `
		INSERT INTO inbox_messages (id, consumer_name, message_id, payload_hash, received_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (consumer_name, message_id) DO NOTHING`,
		message.ID.String(),
		message.ConsumerName,
		message.MessageID,
		message.PayloadHash,
		message.ReceivedAt.UTC(),
	)
	if err != nil {
		return translate("gravar inbox", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrConflict.Messagef("mensagem já registrada")
	}
	return nil
}

// Find devolve a entrega já registrada.
func (r *InboxRepository) Find(ctx context.Context, consumerName, messageID string) (port.InboxMessage, error) {
	var (
		id          string
		name        string
		msgID       string
		payloadHash string
		receivedAt  time.Time
	)
	err := r.db.QueryRow(ctx, `
		SELECT id, consumer_name, message_id, payload_hash, received_at
		FROM inbox_messages
		WHERE consumer_name = $1 AND message_id = $2`,
		consumerName, messageID,
	).Scan(&id, &name, &msgID, &payloadHash, &receivedAt)
	if err != nil {
		return port.InboxMessage{}, translate("consultar inbox", err)
	}

	parsed, err := shared.ParseID(id)
	if err != nil {
		return port.InboxMessage{}, err
	}
	return port.InboxMessage{
		ID:           parsed,
		ConsumerName: name,
		MessageID:    msgID,
		PayloadHash:  payloadHash,
		ReceivedAt:   receivedAt.UTC(),
	}, nil
}
