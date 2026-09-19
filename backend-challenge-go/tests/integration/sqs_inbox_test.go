//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/platform/clock"
	"github.com/vigmi/backend-challenge-go/internal/platform/postgres/repository"
)

func TestInboxRepositoryDeduplicates(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	messageID := "msg-inbox-1"
	err := uow.Execute(ctx, func(ctx context.Context, repos port.Repositories) error {
		id, err := shared.NewID()
		if err != nil {
			return err
		}
		return repos.Inbox().Record(ctx, port.InboxMessage{
			ID:           id,
			ConsumerName: "wager-consumer",
			MessageID:    messageID,
			PayloadHash:  "hash-a",
			ReceivedAt:   time.Now().UTC(),
		})
	})
	if err != nil {
		t.Fatalf("primeira gravação: %v", err)
	}

	err = uow.Execute(ctx, func(ctx context.Context, repos port.Repositories) error {
		id, err := shared.NewID()
		if err != nil {
			return err
		}
		return repos.Inbox().Record(ctx, port.InboxMessage{
			ID:           id,
			ConsumerName: "wager-consumer",
			MessageID:    messageID,
			PayloadHash:  "hash-a",
			ReceivedAt:   time.Now().UTC(),
		})
	})
	if err == nil {
		t.Fatal("esperava conflito na reentrega")
	}
}

func TestHandleWagerMessageRedeliveryDoesNotDoubleDebit(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	ids := clock.UUIDGenerator{}
	sysClock := clock.System{}
	process := usecase.NewProcessWagerTransaction(uow, sysClock, ids)
	open := usecase.NewOpenWallet(uow, sysClock, ids)
	handler := usecase.NewHandleWagerMessage(uow, process, sysClock, ids, "wager-consumer", []string{"provider-a"})

	opened, err := open.Execute(ctx, usecase.OpenWalletCommand{
		PlayerID:       newID(t).String(),
		InitialBalance: usecase.MoneyInput{Amount: "100.00", Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("OpenWallet: %v", err)
	}

	body := envelopeJSON(t, "msg-redeliver", opened.Wallet.PlayerID().String(), opened.Wallet.ID().String(), "sqs-bet-1", "20.00")

	if _, err := handler.Handle(ctx, body); err != nil {
		t.Fatalf("primeira entrega: %v", err)
	}
	// Simula kill após commit e antes do delete: a mesma mensagem volta.
	if _, err := handler.Handle(ctx, body); err != nil {
		t.Fatalf("reentrega: %v", err)
	}

	found, err := uow.ReadOnly().Wallets().FindByID(ctx, opened.Wallet.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Balance().String() != "80.00" {
		t.Fatalf("saldo = %s, esperado 80.00 após uma única aposta", found.Balance())
	}
}

func TestHandleWagerMessageSharesIdempotencyWithHTTP(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	uow := repository.NewUnitOfWork(pool)
	ids := clock.UUIDGenerator{}
	sysClock := clock.System{}
	process := usecase.NewProcessWagerTransaction(uow, sysClock, ids)
	open := usecase.NewOpenWallet(uow, sysClock, ids)
	handler := usecase.NewHandleWagerMessage(uow, process, sysClock, ids, "wager-consumer", []string{"provider-a"})

	opened, err := open.Execute(ctx, usecase.OpenWalletCommand{
		PlayerID:       newID(t).String(),
		InitialBalance: usecase.MoneyInput{Amount: "50.00", Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("OpenWallet: %v", err)
	}

	command := usecase.ProcessTransactionCommand{
		IdempotencyKey:        "provider-a:http-sqs-1",
		ProviderID:            "provider-a",
		ExternalTransactionID: "http-sqs-1",
		PlayerID:              opened.Wallet.PlayerID().String(),
		WalletID:              opened.Wallet.ID().String(),
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  "BET",
		Money:                 usecase.MoneyInput{Amount: "15.00", Currency: "BRL"},
	}
	httpResult, err := process.Execute(ctx, command)
	if err != nil {
		t.Fatalf("HTTP: %v", err)
	}
	if httpResult.Status != wagering.Processed {
		t.Fatalf("HTTP status = %s", httpResult.Status)
	}

	body := envelopeJSON(t, "msg-http-sqs", opened.Wallet.PlayerID().String(), opened.Wallet.ID().String(), "http-sqs-1", "15.00")
	sqsResult, err := handler.Handle(ctx, body)
	if err != nil {
		t.Fatalf("SQS: %v", err)
	}
	if !sqsResult.IdempotentReplay {
		t.Fatalf("esperava replay idempotente no canal SQS, status=%s replay=%v", sqsResult.Status, sqsResult.IdempotentReplay)
	}

	found, err := uow.ReadOnly().Wallets().FindByID(ctx, opened.Wallet.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Balance().String() != "35.00" {
		t.Fatalf("saldo = %s, esperado 35.00", found.Balance())
	}
}

func envelopeJSON(t *testing.T, messageID, playerID, walletID, externalID, amount string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"messageId":  messageID,
		"type":       "WagerTransactionRequested",
		"occurredAt": "2026-09-08T12:00:00.000Z",
		"data": map[string]any{
			"providerId":            "provider-a",
			"externalTransactionId": externalID,
			"idempotencyKey":        "provider-a:" + externalID,
			"playerId":              playerID,
			"walletId":              walletID,
			"roundId":               "round-1",
			"gameId":                "game-1",
			"kind":                  "BET",
			"money":                 map[string]string{"amount": amount, "currency": "BRL"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
