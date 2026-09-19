package usecase

import (
	"context"
	"encoding/json"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
)

// Tipos de evento de integração (ADR-006 / enunciado §11).
const (
	EventWagerTransactionProcessed        = "WagerTransactionProcessed"
	EventWagerTransactionRejected         = "WagerTransactionRejected"
	EventWalletBalanceChanged             = "WalletBalanceChanged"
	EventWagerTransactionPendingReference = "WagerTransactionPendingReference"

	aggregateWallet      = "wallet"
	aggregateTransaction = "transaction"
	eventSchemaVersion   = 1
)

type integrationEnvelope struct {
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	AggregateID   string          `json:"aggregateId"`
	CorrelationID string          `json:"correlationId"`
	CausationID   string          `json:"causationId,omitempty"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Version       int             `json:"version"`
	Data          json.RawMessage `json:"data"`
}

type transactionEventData struct {
	TransactionID         string `json:"transactionId"`
	WalletID              string `json:"walletId"`
	PlayerID              string `json:"playerId"`
	ProviderID            string `json:"providerId,omitempty"`
	ExternalTransactionID string `json:"externalTransactionId,omitempty"`
	Kind                  string `json:"kind"`
	Status                string `json:"status"`
	FailureCode           string `json:"failureCode,omitempty"`
	Amount                any    `json:"money"`
}

type balanceChangedData struct {
	WalletID      string `json:"walletId"`
	TransactionID string `json:"transactionId"`
	Direction     string `json:"direction"`
	Money         any    `json:"money"`
	BalanceBefore any    `json:"balanceBefore"`
	BalanceAfter  any    `json:"balanceAfter"`
	WalletVersion int64  `json:"walletVersion"`
}

func correlationOrNew(ctx context.Context) string {
	if id := correlation.FromContext(ctx); id != "" {
		return id
	}
	return correlation.NewID()
}

func appendProcessedEvents(
	ctx context.Context,
	repositories port.Repositories,
	ids port.IDGenerator,
	transaction *wagering.Transaction,
	applied *settlement,
	now time.Time,
) error {
	if err := appendTransactionEvent(ctx, repositories, ids, transaction,
		EventWagerTransactionProcessed, aggregateTransaction, transaction.ID(), now); err != nil {
		return err
	}
	if applied == nil {
		return nil
	}
	return appendBalanceChanged(ctx, repositories, ids, transaction, applied, now)
}

func appendRejectedEvent(
	ctx context.Context,
	repositories port.Repositories,
	ids port.IDGenerator,
	transaction *wagering.Transaction,
	now time.Time,
) error {
	return appendTransactionEvent(ctx, repositories, ids, transaction,
		EventWagerTransactionRejected, aggregateTransaction, transaction.ID(), now)
}

func appendPendingReferenceEvent(
	ctx context.Context,
	repositories port.Repositories,
	ids port.IDGenerator,
	transaction *wagering.Transaction,
	now time.Time,
) error {
	return appendTransactionEvent(ctx, repositories, ids, transaction,
		EventWagerTransactionPendingReference, aggregateTransaction, transaction.ID(), now)
}

func appendOpeningEvents(
	ctx context.Context,
	repositories port.Repositories,
	ids port.IDGenerator,
	opened *wallet.Wallet,
	opening *wagering.Transaction,
	now time.Time,
) error {
	if err := appendTransactionEvent(ctx, repositories, ids, opening,
		EventWagerTransactionProcessed, aggregateTransaction, opening.ID(), now); err != nil {
		return err
	}
	movement := opened.OpeningMovement()
	return appendBalanceChanged(ctx, repositories, ids, opening, &settlement{
		direction: ledger.Credit,
		movement:  movement,
	}, now)
}

func appendTransactionEvent(
	ctx context.Context,
	repositories port.Repositories,
	ids port.IDGenerator,
	transaction *wagering.Transaction,
	eventType, aggregateType string,
	aggregateID shared.ID,
	now time.Time,
) error {
	data, _ := json.Marshal(transactionEventData{
		TransactionID:         transaction.ID().String(),
		WalletID:              transaction.WalletID().String(),
		PlayerID:              transaction.PlayerID().String(),
		ProviderID:            transaction.ProviderID(),
		ExternalTransactionID: transaction.ExternalID(),
		Kind:                  transaction.Kind().String(),
		Status:                transaction.Status().String(),
		FailureCode:           transaction.FailureCode().String(),
		Amount:                transaction.Amount(),
	})
	return appendOutbox(ctx, repositories, ids, eventType, aggregateType, aggregateID, data, now)
}

func appendBalanceChanged(
	ctx context.Context,
	repositories port.Repositories,
	ids port.IDGenerator,
	transaction *wagering.Transaction,
	applied *settlement,
	now time.Time,
) error {
	data, _ := json.Marshal(balanceChangedData{
		WalletID:      transaction.WalletID().String(),
		TransactionID: transaction.ID().String(),
		Direction:     applied.direction.String(),
		Money:         applied.movement.Amount,
		BalanceBefore: applied.movement.BalanceBefore,
		BalanceAfter:  applied.movement.BalanceAfter,
		WalletVersion: applied.movement.Version,
	})
	return appendOutbox(ctx, repositories, ids, EventWalletBalanceChanged,
		aggregateWallet, transaction.WalletID(), data, now)
}

func appendOutbox(
	ctx context.Context,
	repositories port.Repositories,
	ids port.IDGenerator,
	eventType, aggregateType string,
	aggregateID shared.ID,
	data []byte,
	now time.Time,
) error {
	eventID, err := ids.NewID()
	if err != nil {
		return err
	}
	correlationID := correlationOrNew(ctx)

	envelope, _ := json.Marshal(integrationEnvelope{
		EventID:       eventID.String(),
		EventType:     eventType,
		AggregateID:   aggregateID.String(),
		CorrelationID: correlationID,
		OccurredAt:    now.UTC(),
		Version:       eventSchemaVersion,
		Data:          data,
	})

	return repositories.Outbox().Append(ctx, port.OutboxRecord{
		ID:            eventID,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		EventVersion:  eventSchemaVersion,
		Payload:       envelope,
		CorrelationID: correlationID,
		OccurredAt:    now.UTC(),
		NextAttemptAt: now.UTC(),
	})
}
