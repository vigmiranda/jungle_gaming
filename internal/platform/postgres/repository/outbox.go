package repository

import (
	"context"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// OutboxRepository persiste e reivindica a transactional outbox.
type OutboxRepository struct {
	db querier
}

const outboxReturning = `
	o.id, o.aggregate_type, o.aggregate_id, o.event_type, o.event_version, o.payload,
	o.correlation_id, o.causation_id, o.occurred_at, o.attempts, o.next_attempt_at,
	o.locked_by, o.locked_until, o.published_at`

// Append grava o evento no mesmo commit do domínio.
func (r *OutboxRepository) Append(ctx context.Context, record port.OutboxRecord) error {
	var causation any
	if record.CausationID != "" {
		causation = record.CausationID
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO outbox_events (
			id, aggregate_type, aggregate_id, event_type, event_version, payload,
			correlation_id, causation_id, occurred_at, next_attempt_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		record.ID.String(),
		record.AggregateType,
		record.AggregateID.String(),
		record.EventType,
		record.EventVersion,
		record.Payload,
		record.CorrelationID,
		causation,
		record.OccurredAt.UTC(),
		record.NextAttemptAt.UTC(),
	)
	return translate("gravar outbox", err)
}

// Claim reserva um lote com SKIP LOCKED e lease com TTL (ADR-006).
func (r *OutboxRepository) Claim(
	ctx context.Context,
	publisherID string,
	limit int,
	leaseTTL time.Duration,
	now time.Time,
) ([]port.OutboxRecord, error) {
	leaseUntil := now.UTC().Add(leaseTTL)
	rows, err := r.db.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM outbox_events
			WHERE published_at IS NULL
			  AND next_attempt_at <= $1
			  AND (locked_until IS NULL OR locked_until < $1)
			ORDER BY next_attempt_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events AS o
		SET locked_by = $3, locked_until = $4
		FROM candidates AS c
		WHERE o.id = c.id
		RETURNING `+outboxReturning,
		now.UTC(), limit, publisherID, leaseUntil,
	)
	if err != nil {
		return nil, translate("reivindicar outbox", err)
	}
	defer rows.Close()

	var claimed []port.OutboxRecord
	for rows.Next() {
		record, scanErr := scanOutbox(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		claimed = append(claimed, record)
	}
	if err := rows.Err(); err != nil {
		return nil, translate("reivindicar outbox", err)
	}
	return claimed, nil
}

// MarkPublished confirma a publicação e libera o lease.
func (r *OutboxRepository) MarkPublished(ctx context.Context, id shared.ID, now time.Time) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE outbox_events
		SET published_at = $2, locked_by = NULL, locked_until = NULL
		WHERE id = $1 AND published_at IS NULL`,
		id.String(), now.UTC(),
	)
	if err != nil {
		return translate("confirmar outbox", err)
	}
	return requireSingleRow("confirmar outbox", tag)
}

// ReleaseWithBackoff libera o lease e agenda a próxima tentativa.
func (r *OutboxRepository) ReleaseWithBackoff(
	ctx context.Context,
	id shared.ID,
	attempts int,
	nextAttemptAt, now time.Time,
) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE outbox_events
		SET attempts = $2, next_attempt_at = $3, locked_by = NULL, locked_until = NULL
		WHERE id = $1 AND published_at IS NULL`,
		id.String(), attempts, nextAttemptAt.UTC(),
	)
	if err != nil {
		return translate("reagendar outbox", err)
	}
	_ = now
	return requireSingleRow("reagendar outbox", tag)
}

func scanOutbox(row scannable) (port.OutboxRecord, error) {
	var (
		id, aggregateID, aggregateType, eventType, correlationID string
		causationID                                              *string
		eventVersion, attempts                                   int
		payload                                                  []byte
		occurredAt, nextAttemptAt                                time.Time
		lockedBy                                                 *string
		lockedUntil, publishedAt                                 *time.Time
	)
	if err := row.Scan(
		&id, &aggregateType, &aggregateID, &eventType, &eventVersion, &payload,
		&correlationID, &causationID, &occurredAt, &attempts, &nextAttemptAt,
		&lockedBy, &lockedUntil, &publishedAt,
	); err != nil {
		return port.OutboxRecord{}, translate("ler outbox", err)
	}

	parsedID, err := shared.ParseID(id)
	if err != nil {
		return port.OutboxRecord{}, err
	}
	parsedAggregate, err := shared.ParseID(aggregateID)
	if err != nil {
		return port.OutboxRecord{}, err
	}

	record := port.OutboxRecord{
		ID:            parsedID,
		AggregateType: aggregateType,
		AggregateID:   parsedAggregate,
		EventType:     eventType,
		EventVersion:  eventVersion,
		Payload:       payload,
		CorrelationID: correlationID,
		OccurredAt:    occurredAt.UTC(),
		Attempts:      attempts,
		NextAttemptAt: nextAttemptAt.UTC(),
	}
	if causationID != nil {
		record.CausationID = *causationID
	}
	if lockedBy != nil {
		record.LockedBy = *lockedBy
		record.HasLease = true
	}
	if lockedUntil != nil {
		record.LockedUntil = lockedUntil.UTC()
	}
	if publishedAt != nil {
		record.PublishedAt = publishedAt.UTC()
		record.HasPublished = true
	}
	return record, nil
}
