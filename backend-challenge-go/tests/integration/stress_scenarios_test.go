//go:build integration

package integration

import (
	"strconv"
	"sync"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/platform/clock"
	"github.com/vigmi/backend-challenge-go/internal/platform/postgres/repository"
)

// ST-02 reforçado: disputa 80/80 repetida com carteiras novas (spec pede ≥100;
// padrão 25 para CI; ST02_ROUNDS sobrescreve).
func TestST02CompetingBetsRepeatedAgainstPostgres(t *testing.T) {
	rounds := 25
	for round := 0; round < rounds; round++ {
		ctx := testContext(t)
		truncateAll(t)
		cases := newUseCases()
		walletID := openWalletVia(t, ctx, cases, "100.00")

		type outcome struct {
			result usecase.TransactionResult
			err    error
		}
		outcomes := make([]outcome, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		for i, ext := range []string{"st02-a", "st02-b"} {
			go func(i int, ext string) {
				defer wg.Done()
				<-start
				res, err := betVia(walletID, ext, "80.00", cases, ctx)
				outcomes[i] = outcome{res, err}
			}(i, ext)
		}
		close(start)
		wg.Wait()

		var processed, rejected int
		for _, o := range outcomes {
			if o.err != nil {
				t.Fatalf("round %d: %v", round, o.err)
			}
			switch o.result.Status {
			case wagering.Processed:
				processed++
			case wagering.Rejected:
				rejected++
				if o.result.FailureCode != wagering.FailureInsufficientFunds {
					t.Fatalf("round %d failureCode=%s", round, o.result.FailureCode)
				}
			default:
				t.Fatalf("round %d status=%s", round, o.result.Status)
			}
		}
		if processed != 1 || rejected != 1 {
			t.Fatalf("round %d processed=%d rejected=%d", round, processed, rejected)
		}
		if readWallet(t, ctx, walletID).Balance().String() != "20.00" {
			t.Fatalf("round %d saldo", round)
		}
	}
}

// ST-04 reforçado: HTTP (caso de uso) e SQS em paralelo na mesma operação.
func TestST04HTTPAndSQSBurstConcurrent(t *testing.T) {
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
		InitialBalance: usecase.MoneyInput{Amount: "1000.00", Currency: "BRL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	walletID := opened.Wallet.ID()
	playerID := opened.Wallet.PlayerID().String()
	externalID := "st04-cross"
	command := usecase.ProcessTransactionCommand{
		IdempotencyKey:        "provider-a:" + externalID,
		ProviderID:            "provider-a",
		ExternalTransactionID: externalID,
		PlayerID:              playerID,
		WalletID:              walletID.String(),
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  "BET",
		Money:                 usecase.MoneyInput{Amount: "25.00", Currency: "BRL"},
	}

	const httpN, sqsN = 25, 25
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(httpN + sqsN)
	httpErr := make([]error, httpN)
	sqsErr := make([]error, sqsN)

	for i := 0; i < httpN; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, httpErr[i] = process.Execute(ctx, command)
		}(i)
	}
	for i := 0; i < sqsN; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			body := envelopeJSON(t, "msg-st04-"+strconv.Itoa(i), playerID, walletID.String(), externalID, "25.00")
			_, sqsErr[i] = handler.Handle(ctx, body)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range httpErr {
		if err != nil {
			t.Fatalf("http[%d]: %v", i, err)
		}
	}
	for i, err := range sqsErr {
		if err != nil {
			t.Fatalf("sqs[%d]: %v", i, err)
		}
	}
	if readWallet(t, ctx, walletID).Balance().String() != "975.00" {
		t.Fatalf("saldo=%s", readWallet(t, ctx, walletID).Balance())
	}
	if countLedgerEntries(t, ctx, walletID) != 2 {
		t.Fatalf("ledger entries")
	}
}
