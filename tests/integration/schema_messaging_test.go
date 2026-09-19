//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// collectIDs executa a reivindicação dentro da transação informada.
func collectIDs(ctx context.Context, tx pgx.Tx, query string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// A inbox é a deduplicação da aplicação, independente do SQS FIFO: a mesma
// mensagem reentregue não pode ser tratada duas vezes.
func TestInboxDeduplicatesByConsumerAndMessage(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	insert := func(consumer, messageID string) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO inbox_messages (id, consumer_name, message_id, payload_hash, received_at)
			VALUES ($1, $2, $3, $4, $5)`,
			newUUID(t), consumer, messageID, "hash", time.Now().UTC())
		return err
	}

	if err := insert("wager-consumer", "msg-123"); err != nil {
		t.Fatalf("primeira entrega deveria ser aceita: %v", err)
	}

	if err := insert("wager-consumer", "msg-123"); err == nil {
		t.Fatal("esperava recusa da reentrega para o mesmo consumidor")
	} else {
		assertConstraint(t, err, "inbox_messages_consumer_message_uq")
	}

	// Consumidores diferentes tratam a mesma mensagem de forma independente.
	if err := insert("audit-consumer", "msg-123"); err != nil {
		t.Fatalf("outro consumidor deveria poder tratar a mesma mensagem: %v", err)
	}
}

func TestOutboxConstraints(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	insert := func(mutate func(*outboxRow)) error {
		row := newOutboxRow(t)
		if mutate != nil {
			mutate(&row)
		}
		return insertOutboxEvent(ctx, row)
	}

	t.Run("evento válido é aceito", func(t *testing.T) {
		if err := insert(nil); err != nil {
			t.Fatalf("evento válido deveria ser aceito: %v", err)
		}
	})

	// Um registro com dono e sem prazo ficaria travado para sempre; com prazo e
	// sem dono, ninguém saberia quem o reivindicou.
	t.Run("lease é gravado por inteiro", func(t *testing.T) {
		if err := insert(func(r *outboxRow) { r.LockedBy = text("publisher-1") }); err == nil {
			t.Error("esperava recusa: lease sem prazo")
		} else {
			assertConstraint(t, err, "outbox_events_lease_consistency")
		}

		until := time.Now().UTC().Add(time.Minute)
		if err := insert(func(r *outboxRow) { r.LockedUntil = &until }); err == nil {
			t.Error("esperava recusa: prazo sem dono")
		} else {
			assertConstraint(t, err, "outbox_events_lease_consistency")
		}
	})

	t.Run("lease completo é aceito", func(t *testing.T) {
		until := time.Now().UTC().Add(time.Minute)

		err := insert(func(r *outboxRow) {
			r.LockedBy = text("publisher-1")
			r.LockedUntil = &until
		})

		if err != nil {
			t.Fatalf("lease completo deveria ser aceito: %v", err)
		}
	})

	t.Run("versão do evento é positiva", func(t *testing.T) {
		if err := insert(func(r *outboxRow) { r.EventVersion = 0 }); err == nil {
			t.Fatal("esperava recusa do CHECK de versão")
		} else {
			assertConstraint(t, err, "outbox_events_event_version_positive")
		}
	})

	// O eventId é estável: uma republicação reaproveita o mesmo identificador,
	// então a chave primária é o que impede duplicar o registro.
	t.Run("eventId é único", func(t *testing.T) {
		row := newOutboxRow(t)
		if err := insertOutboxEvent(ctx, row); err != nil {
			t.Fatalf("primeiro evento deveria ser aceito: %v", err)
		}

		if err := insertOutboxEvent(ctx, row); err == nil {
			t.Fatal("esperava recusa do evento duplicado")
		} else {
			assertConstraint(t, err, "outbox_events_pkey")
		}
	})
}

// O publisher reivindica lotes com SKIP LOCKED; o índice parcial cobre
// exatamente os pendentes elegíveis.
func TestOutboxClaimSkipsLockedRows(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	for index := 0; index < 3; index++ {
		if err := insertOutboxEvent(ctx, newOutboxRow(t)); err != nil {
			t.Fatalf("não foi possível inserir o evento: %v", err)
		}
	}

	const claim = `
		SELECT id FROM outbox_events
		WHERE published_at IS NULL AND next_attempt_at <= now()
		ORDER BY next_attempt_at, id
		LIMIT 2
		FOR UPDATE SKIP LOCKED`

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("não foi possível abrir a primeira transação: %v", err)
	}
	defer func() { _ = first.Rollback(ctx) }()

	firstClaim, err := collectIDs(ctx, first, claim)
	if err != nil {
		t.Fatalf("primeira reivindicação falhou: %v", err)
	}
	if len(firstClaim) != 2 {
		t.Fatalf("primeira reivindicação trouxe %d eventos, esperados 2", len(firstClaim))
	}

	second, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("não foi possível abrir a segunda transação: %v", err)
	}
	defer func() { _ = second.Rollback(ctx) }()

	secondClaim, err := collectIDs(ctx, second, claim)
	if err != nil {
		t.Fatalf("segunda reivindicação falhou: %v", err)
	}

	// O segundo publisher pega o que sobrou, sem esperar e sem duplicar.
	if len(secondClaim) != 1 {
		t.Fatalf("segunda reivindicação trouxe %d eventos, esperado 1", len(secondClaim))
	}
	for _, claimed := range secondClaim {
		for _, taken := range firstClaim {
			if claimed == taken {
				t.Errorf("evento %s foi reivindicado pelos dois publishers", claimed)
			}
		}
	}
}

type outboxRow struct {
	ID            uuid.UUID
	AggregateType string
	AggregateID   uuid.UUID
	EventType     string
	EventVersion  int
	Payload       string
	CorrelationID string
	LockedBy      *string
	LockedUntil   *time.Time
}

func newOutboxRow(t *testing.T) outboxRow {
	t.Helper()
	return outboxRow{
		ID:            newUUID(t),
		AggregateType: "wallet",
		AggregateID:   newUUID(t),
		EventType:     "WalletBalanceChanged",
		EventVersion:  1,
		Payload:       `{"walletId":"0192f291-27dd-7d3f-8071-5f8685deef37"}`,
		CorrelationID: newUUID(t).String(),
	}
}

func insertOutboxEvent(ctx context.Context, row outboxRow) error {
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		INSERT INTO outbox_events (
			id, aggregate_type, aggregate_id, event_type, event_version, payload,
			correlation_id, occurred_at, next_attempt_at, locked_by, locked_until
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9, $10)`,
		row.ID, row.AggregateType, row.AggregateID, row.EventType, row.EventVersion, row.Payload,
		row.CorrelationID, now, row.LockedBy, row.LockedUntil)
	return err
}
