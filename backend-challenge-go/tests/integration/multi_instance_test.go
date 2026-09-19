//go:build integration

package integration

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/platform/clock"
	"github.com/vigmi/backend-challenge-go/internal/platform/postgres/repository"
)

// Três pilhas de caso de uso independentes (UoW/conexões distintas) contra o
// mesmo PostgreSQL — evidência de que idempotência e lock não vivem na memória
// de um único processo (ADR-020 / cenário 4 do enunciado).
func TestThreeIndependentInstancesShareIdempotency(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	const instances = 3
	stacks := make([]useCases, instances)
	for i := range stacks {
		uow := repository.NewUnitOfWork(pool)
		sysClock := clock.System{}
		ids := clock.UUIDGenerator{}
		stacks[i] = useCases{
			open:      usecase.NewOpenWallet(uow, sysClock, ids),
			process:   usecase.NewProcessWagerTransaction(uow, sysClock, ids),
			reconcile: usecase.NewReconcileWallet(uow),
		}
	}

	walletID := openWalletVia(t, ctx, stacks[0], "1000.00")

	const attempts = 30
	results := make([]usecase.TransactionResult, attempts)
	failures := make([]error, attempts)
	start := make(chan struct{})

	var waitGroup sync.WaitGroup
	waitGroup.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer waitGroup.Done()
			<-start
			stack := stacks[index%instances]
			results[index], failures[index] = betVia(walletID, "multi-instance-bet", "25.00", stack, ctx)
		}(index)
	}
	close(start)
	waitGroup.Wait()

	var processed, replayed int
	for index, err := range failures {
		if err != nil {
			t.Fatalf("tentativa %d: %v", index, err)
		}
		if results[index].IdempotentReplay {
			replayed++
			continue
		}
		if results[index].Status != wagering.Processed {
			t.Fatalf("tentativa %d status = %s", index, results[index].Status)
		}
		processed++
	}

	if processed != 1 || replayed != attempts-1 {
		t.Fatalf("processadas=%d replays=%d (esperado 1 e %d)", processed, replayed, attempts-1)
	}
	if balance := readWallet(t, ctx, walletID).Balance().String(); balance != "975.00" {
		t.Errorf("saldo = %s, esperado 975.00", balance)
	}
	if entries := countLedgerEntries(t, ctx, walletID); entries != 2 {
		t.Errorf("lançamentos = %d, esperados 2", entries)
	}

	reconcile, err := stacks[1].reconcile.Execute(ctx, walletID)
	if err != nil {
		t.Fatalf("reconciliação: %v", err)
	}
	if !reconcile.Consistent {
		t.Fatalf("divergência após multi-instância: %+v", reconcile)
	}
}

// Provedor fora da allow-list via SQS não altera saldo (política do consumidor).
func TestUnauthorizedSQSProviderLeavesWalletUntouched(t *testing.T) {
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

	body := envelopeJSONForProvider(t, "msg-bad-provider", "provider-evil",
		opened.Wallet.PlayerID().String(), opened.Wallet.ID().String(), "evil-bet", "40.00")

	_, err = handler.Handle(ctx, body)
	if err == nil {
		t.Fatal("esperava rejeição de provedor não autorizado")
	}
	if !errors.Is(err, usecase.ErrUnauthorizedProvider) {
		t.Fatalf("erro = %v, esperado ErrUnauthorizedProvider", err)
	}

	found, err := uow.ReadOnly().Wallets().FindByID(ctx, opened.Wallet.ID())
	if err != nil {
		t.Fatal(err)
	}
	if found.Balance().String() != "100.00" {
		t.Fatalf("saldo = %s, não deveria ter mudado", found.Balance())
	}
	if entries := countLedgerEntries(t, ctx, opened.Wallet.ID()); entries != 1 {
		t.Fatalf("lançamentos = %d, esperado só a abertura", entries)
	}
}

func envelopeJSONForProvider(t *testing.T, messageID, providerID, playerID, walletID, externalID, amount string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"messageId":  messageID,
		"type":       "WagerTransactionRequested",
		"occurredAt": "2026-09-08T12:00:00.000Z",
		"data": map[string]any{
			"providerId":            providerID,
			"externalTransactionId": externalID,
			"idempotencyKey":        providerID + ":" + externalID,
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
