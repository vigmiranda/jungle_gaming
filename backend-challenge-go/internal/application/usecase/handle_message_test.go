package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

func TestHandleWagerMessageProcessesAndDeduplicates(t *testing.T) {
	uow := newFakeUnitOfWork()
	clock := &fakeClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	ids := &fakeIDs{}
	process := usecase.NewProcessWagerTransaction(uow, clock, ids)
	handler := usecase.NewHandleWagerMessage(uow, process, clock, ids, "wager-consumer", []string{"provider-a"})

	opened := openTestWallet(t, uow, clock, ids, "1000.00")
	body := mustEnvelope(t, "msg-1", opened, "bet-sqs-1", "25.00")

	result, err := handler.Handle(context.Background(), body)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Status != wagering.Processed {
		t.Fatalf("status = %s", result.Status)
	}

	balance := mustWalletBalance(t, uow, opened)
	if balance.String() != "975.00" {
		t.Fatalf("saldo após primeira entrega = %s", balance)
	}

	_, err = handler.Handle(context.Background(), body)
	if err != nil {
		t.Fatalf("reentrega: %v", err)
	}
	balance = mustWalletBalance(t, uow, opened)
	if balance.String() != "975.00" {
		t.Fatalf("saldo após reentrega = %s, esperado 975.00", balance)
	}
	if len(uow.state.ledger) != 2 {
		t.Fatalf("lançamentos = %d, esperados 2", len(uow.state.ledger))
	}
}

func TestHandleWagerMessageRejectsInvalidEnvelope(t *testing.T) {
	handler := newMessageHandler(t, newFakeUnitOfWork())
	_, err := handler.Handle(context.Background(), []byte(`{`))
	if err == nil {
		t.Fatal("esperava envelope inválido")
	}
}

func TestHandleWagerMessageRejectsUnauthorizedProvider(t *testing.T) {
	handler := newMessageHandler(t, newFakeUnitOfWork())
	body := []byte(`{
		"messageId":"msg-x","type":"WagerTransactionRequested","occurredAt":"2026-09-08T12:00:00.000Z",
		"data":{"providerId":"provider-z","externalTransactionId":"e1","idempotencyKey":"k",
			"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","walletId":"0192f291-27dd-7d3f-8071-5f8685deef37",
			"roundId":"r","gameId":"g","kind":"BET","money":{"amount":"1.00","currency":"BRL"}}
	}`)
	if _, err := handler.Handle(context.Background(), body); err == nil {
		t.Fatal("esperava rejeitar provider não autorizado")
	}
}

func TestHandleWagerMessageDetectsPayloadHashMismatch(t *testing.T) {
	uow := newFakeUnitOfWork()
	handler := newMessageHandler(t, uow)
	opened := openTestWallet(t, uow, &fakeClock{now: time.Now().UTC()}, &fakeIDs{}, "100.00")
	body := mustEnvelope(t, "msg-hash", opened, "hash-1", "10.00")
	if _, err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("primeira: %v", err)
	}
	altered := mustEnvelope(t, "msg-hash", opened, "hash-1", "11.00")
	if _, err := handler.Handle(context.Background(), altered); err == nil {
		t.Fatal("esperava mismatch de hash")
	}
}

func TestHandleWagerMessagePropagatesTransientErrors(t *testing.T) {
	uow := newFakeUnitOfWork()
	handler := newMessageHandler(t, uow)
	opened := openTestWallet(t, uow, &fakeClock{now: time.Now().UTC()}, &fakeIDs{}, "100.00")
	uow.state.walletLockErr = errors.New("postgres indisponível")
	if _, err := handler.Handle(context.Background(), mustEnvelope(t, "msg-tmp", opened, "tmp-1", "5.00")); err == nil {
		t.Fatal("esperava erro transitório")
	}
}

func TestHandleWagerMessagePropagatesInboxFindErrors(t *testing.T) {
	uow := newFakeUnitOfWork()
	handler := newMessageHandler(t, uow)
	opened := openTestWallet(t, uow, &fakeClock{now: time.Now().UTC()}, &fakeIDs{}, "100.00")
	uow.state.inboxFindErr = errors.New("inbox offline")
	if _, err := handler.Handle(context.Background(), mustEnvelope(t, "msg-find", opened, "find-1", "5.00")); err == nil {
		t.Fatal("esperava falha ao consultar inbox")
	}
}

func TestHandleWagerMessageRaceOnInboxRecord(t *testing.T) {
	uow := newFakeUnitOfWork()
	handler := newMessageHandler(t, uow)
	opened := openTestWallet(t, uow, &fakeClock{now: time.Now().UTC()}, &fakeIDs{}, "100.00")
	body := mustEnvelope(t, "msg-race", opened, "race-1", "5.00")

	if _, err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("primeira: %v", err)
	}
	stored := uow.state.inbox[inboxKey("wager-consumer", "msg-race")]

	uow.state.inboxFindMisses = 1
	uow.state.inboxRecordErr = port.ErrConflict
	uow.state.inbox[inboxKey("wager-consumer", "msg-race")] = stored

	if _, err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("corrida: %v", err)
	}
}

func TestHandleWagerMessageRaceHashMismatch(t *testing.T) {
	uow := newFakeUnitOfWork()
	handler := newMessageHandler(t, uow)
	opened := openTestWallet(t, uow, &fakeClock{now: time.Now().UTC()}, &fakeIDs{}, "100.00")
	body := mustEnvelope(t, "msg-race-bad", opened, "race-bad", "5.00")

	id, err := shared.NewID()
	if err != nil {
		t.Fatal(err)
	}
	uow.state.inboxFindMisses = 1
	uow.state.inboxRecordErr = port.ErrConflict
	uow.state.inbox[inboxKey("wager-consumer", "msg-race-bad")] = port.InboxMessage{
		ID: id, ConsumerName: "wager-consumer", MessageID: "msg-race-bad",
		PayloadHash: "hash-divergente", ReceivedAt: time.Now().UTC(),
	}

	if _, err := handler.Handle(context.Background(), body); err == nil {
		t.Fatal("esperava mismatch na corrida")
	}
}

func TestHandleWagerMessageRejectsMalformedEnvelope(t *testing.T) {
	handler := newMessageHandler(t, newFakeUnitOfWork())
	cases := []string{
		`{"messageId":"","type":"WagerTransactionRequested","data":{}}`,
		`{"messageId":"m","type":"Other","occurredAt":"2026-09-08T12:00:00.000Z","data":{}}`,
		`{"messageId":"m","type":"WagerTransactionRequested","occurredAt":"2026-09-08T12:00:00.000Z","data":"x"}`,
	}
	for _, raw := range cases {
		if _, err := handler.Handle(context.Background(), []byte(raw)); err == nil {
			t.Fatalf("esperava rejeitar envelope %s", raw)
		}
	}
}

func TestHandleWagerMessageCrossChannelWithHTTP(t *testing.T) {
	uow := newFakeUnitOfWork()
	clock := &fakeClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	ids := &fakeIDs{}
	process := usecase.NewProcessWagerTransaction(uow, clock, ids)
	handler := usecase.NewHandleWagerMessage(uow, process, clock, ids, "wager-consumer", []string{"provider-a"})

	opened := openTestWallet(t, uow, clock, ids, "100.00")
	command := usecase.ProcessTransactionCommand{
		IdempotencyKey: "provider-a:cross-1", ProviderID: "provider-a",
		ExternalTransactionID: "cross-1", PlayerID: opened.PlayerID().String(),
		WalletID: opened.ID().String(), RoundID: "round-1", GameID: "game-1",
		Kind: "BET", Money: usecase.MoneyInput{Amount: "10.00", Currency: "BRL"},
	}

	if _, err := process.Execute(context.Background(), command); err != nil {
		t.Fatalf("HTTP: %v", err)
	}

	sqsResult, err := handler.Handle(context.Background(), mustEnvelope(t, "msg-cross", opened, "cross-1", "10.00"))
	if err != nil {
		t.Fatalf("SQS: %v", err)
	}
	if !sqsResult.IdempotentReplay {
		t.Fatalf("esperava replay no SQS, status=%s", sqsResult.Status)
	}
	if mustWalletBalance(t, uow, opened).String() != "90.00" {
		t.Fatalf("saldo cruzado inesperado")
	}
}

func TestHandleWagerMessageRecordFailure(t *testing.T) {
	uow := newFakeUnitOfWork()
	handler := newMessageHandler(t, uow)
	opened := openTestWallet(t, uow, &fakeClock{now: time.Now().UTC()}, &fakeIDs{}, "100.00")
	uow.state.inboxRecordErr = errors.New("disco cheio")
	if _, err := handler.Handle(context.Background(), mustEnvelope(t, "msg-rec", opened, "rec-1", "5.00")); err == nil {
		t.Fatal("esperava falha ao gravar inbox")
	}
}

func TestHandleWagerMessageTerminalBusinessErrorStillCommitsInbox(t *testing.T) {
	uow := newFakeUnitOfWork()
	handler := newMessageHandler(t, uow)
	// Sem carteira: ExecuteIn falha de forma permanente; a inbox permanece.
	body := []byte(`{
		"messageId":"msg-term","type":"WagerTransactionRequested","occurredAt":"2026-09-08T12:00:00.000Z",
		"data":{"providerId":"provider-a","externalTransactionId":"term-1","idempotencyKey":"provider-a:term-1",
			"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","walletId":"0192f291-27dd-7d3f-8071-5f8685deef37",
			"roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"5.00","currency":"BRL"}}
	}`)
	if _, err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("erro terminal deveria ser absorvido após gravar inbox: %v", err)
	}
	if _, ok := uow.state.inbox[inboxKey("wager-consumer", "msg-term")]; !ok {
		t.Fatal("inbox deveria ter sido gravada")
	}
}

func TestHandleWagerMessageRaceFindAfterConflictFails(t *testing.T) {
	uow := newFakeUnitOfWork()
	handler := newMessageHandler(t, uow)
	opened := openTestWallet(t, uow, &fakeClock{now: time.Now().UTC()}, &fakeIDs{}, "100.00")
	body := mustEnvelope(t, "msg-race-find", opened, "race-find", "5.00")

	uow.state.inboxFindMisses = 1
	uow.state.inboxRecordErr = port.ErrConflict
	// Sem linha plantada: o Find após conflito devolve NotFound.
	if _, err := handler.Handle(context.Background(), body); err == nil {
		t.Fatal("esperava falha ao reinspecionar inbox após conflito")
	}
}

func TestHandleWagerMessageIDGenerationFailure(t *testing.T) {
	uow := newFakeUnitOfWork()
	clock := &fakeClock{now: time.Now().UTC()}
	ids := &fakeIDs{}
	opened := openTestWallet(t, uow, clock, ids, "100.00")
	ids.failAfter = ids.calls + 1
	handler := usecase.NewHandleWagerMessage(
		uow, usecase.NewProcessWagerTransaction(uow, clock, ids), clock, ids,
		"wager-consumer", []string{"provider-a"},
	)
	if _, err := handler.Handle(context.Background(), mustEnvelope(t, "msg-id", opened, "id-1", "5.00")); err == nil {
		t.Fatal("esperava falha ao gerar id da inbox")
	}
}

func TestNewHandleWagerMessageFromConfig(t *testing.T) {
	uow := newFakeUnitOfWork()
	clock := &fakeClock{now: time.Now().UTC()}
	ids := &fakeIDs{}
	process := usecase.NewProcessWagerTransaction(uow, clock, ids)
	handler := usecase.NewHandleWagerMessageFromConfig(uow, process, clock, ids, config.Config{
		SQS: config.SQS{ConsumerName: "from-config", AllowedProviders: []string{"provider-a"}},
	})
	if handler == nil {
		t.Fatal("handler nulo")
	}
}

func newMessageHandler(t *testing.T, uow *fakeUnitOfWork) *usecase.HandleWagerMessage {
	t.Helper()
	clock := &fakeClock{now: time.Now().UTC()}
	ids := &fakeIDs{}
	return usecase.NewHandleWagerMessage(
		uow, usecase.NewProcessWagerTransaction(uow, clock, ids), clock, ids,
		"wager-consumer", []string{"provider-a"},
	)
}

func openTestWallet(t *testing.T, uow *fakeUnitOfWork, clock *fakeClock, ids *fakeIDs, amount string) *wallet.Wallet {
	t.Helper()
	result, err := usecase.NewOpenWallet(uow, clock, ids).Execute(context.Background(), usecase.OpenWalletCommand{
		PlayerID:       "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
		InitialBalance: usecase.MoneyInput{Amount: amount, Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("OpenWallet: %v", err)
	}
	return result.Wallet
}

func mustWalletBalance(t *testing.T, uow *fakeUnitOfWork, target *wallet.Wallet) money.Money {
	t.Helper()
	record, ok := uow.state.wallets[target.ID().String()]
	if !ok {
		t.Fatal("carteira ausente")
	}
	return record.wallet.Balance()
}

func mustEnvelope(t *testing.T, messageID string, opened *wallet.Wallet, externalID, amount string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"messageId": messageID, "type": "WagerTransactionRequested", "occurredAt": "2026-09-08T12:00:00.000Z",
		"data": map[string]any{
			"providerId": "provider-a", "externalTransactionId": externalID,
			"idempotencyKey": "provider-a:" + externalID,
			"playerId":       opened.PlayerID().String(), "walletId": opened.ID().String(),
			"roundId": "round-1", "gameId": "game-1", "kind": "BET",
			"money": map[string]string{"amount": amount, "currency": "BRL"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
