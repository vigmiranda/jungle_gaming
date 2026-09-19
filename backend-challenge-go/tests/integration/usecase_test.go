//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/platform/clock"
)

type useCases struct {
	open      *usecase.OpenWallet
	process   *usecase.ProcessWagerTransaction
	reconcile *usecase.ReconcileWallet
}

func newUseCases() useCases {
	unitOfWork := newUnitOfWork()
	systemClock := clock.System{}
	ids := clock.UUIDGenerator{}

	return useCases{
		open:      usecase.NewOpenWallet(unitOfWork, systemClock, ids),
		process:   usecase.NewProcessWagerTransaction(unitOfWork, systemClock, ids),
		reconcile: usecase.NewReconcileWallet(unitOfWork),
	}
}

func openWalletVia(t *testing.T, ctx context.Context, cases useCases, balance string) shared.ID {
	t.Helper()

	playerID, err := shared.NewID()
	if err != nil {
		t.Fatalf("não foi possível gerar o jogador: %v", err)
	}

	result, err := cases.open.Execute(ctx, usecase.OpenWalletCommand{
		PlayerID:       playerID.String(),
		InitialBalance: usecase.MoneyInput{Amount: balance, Currency: "BRL"},
	})
	if err != nil {
		t.Fatalf("abertura devolveu erro: %v", err)
	}
	return result.Wallet.ID()
}

func betVia(walletID shared.ID, externalID, amount string, cases useCases, ctx context.Context) (usecase.TransactionResult, error) {
	playerID := playerOf(ctx, walletID)

	return cases.process.Execute(ctx, usecase.ProcessTransactionCommand{
		IdempotencyKey:        "provider-a:" + externalID,
		ProviderID:            "provider-a",
		ExternalTransactionID: externalID,
		PlayerID:              playerID,
		WalletID:              walletID.String(),
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  "BET",
		Money:                 usecase.MoneyInput{Amount: amount, Currency: "BRL"},
	})
}

func playerOf(ctx context.Context, walletID shared.ID) string {
	var playerID string
	_ = pool.QueryRow(ctx, `SELECT player_id FROM wallets WHERE id = $1`, walletID.String()).Scan(&playerID)
	return playerID
}

// A abertura grava carteira, OPENING processada e o crédito no mesmo commit.
func TestOpenWalletPersistsOpeningAndLedger(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	cases := newUseCases()

	walletID := openWalletVia(t, ctx, cases, "1000.00")

	stored := readWallet(t, ctx, walletID)
	if stored.Balance().String() != "1000.00" || stored.Version() != 1 {
		t.Errorf("carteira = %s / versão %d", stored.Balance(), stored.Version())
	}
	if entries := countLedgerEntries(t, ctx, walletID); entries != 1 {
		t.Errorf("lançamentos = %d, esperado 1", entries)
	}

	var kind, status, origin string
	if err := pool.QueryRow(ctx,
		`SELECT kind, status, origin FROM wager_transactions WHERE wallet_id = $1`,
		walletID.String()).Scan(&kind, &status, &origin); err != nil {
		t.Fatalf("não foi possível ler a abertura: %v", err)
	}
	if kind != "OPENING" || status != "PROCESSED" || origin != "INTERNAL" {
		t.Errorf("abertura = %s / %s / %s", kind, status, origin)
	}
}

// Cenário obrigatório do desafio: a mesma aposta enviada 50 vezes em paralelo
// produz um único débito.
func TestSameBetSentFiftyTimesInParallelDebitsOnce(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	cases := newUseCases()
	walletID := openWalletVia(t, ctx, cases, "1000.00")

	const attempts = 50
	results := make([]usecase.TransactionResult, attempts)
	failures := make([]error, attempts)
	start := make(chan struct{})

	var waitGroup sync.WaitGroup
	waitGroup.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer waitGroup.Done()
			<-start
			results[index], failures[index] = betVia(walletID, "transaction-1", "25.00", cases, ctx)
		}(index)
	}
	close(start)
	waitGroup.Wait()

	var processed, replayed int
	for index, err := range failures {
		if err != nil {
			t.Fatalf("tentativa %d devolveu erro: %v", index, err)
		}
		if results[index].IdempotentReplay {
			replayed++
			continue
		}
		processed++
	}

	if processed != 1 || replayed != attempts-1 {
		t.Errorf("resultados = %d processadas e %d replays, esperado 1 e %d", processed, replayed, attempts-1)
	}
	if balance := readWallet(t, ctx, walletID).Balance().String(); balance != "975.00" {
		t.Errorf("saldo = %s, esperado 975.00", balance)
	}
	if entries := countLedgerEntries(t, ctx, walletID); entries != 2 {
		t.Errorf("lançamentos = %d, esperados 2 (abertura e a única aposta)", entries)
	}

	// Todas as respostas apontam para a mesma transação.
	for index, result := range results {
		if !result.TransactionID.Equal(results[0].TransactionID) {
			t.Fatalf("tentativa %d devolveu outra transação: %s", index, result.TransactionID)
		}
	}
}

// Cenário obrigatório: duas apostas de 80.00 sobre um saldo de 100.00.
func TestTwoCompetingBetsLeaveOneProcessedAndOneRejected(t *testing.T) {
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

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	for index, externalID := range []string{"bet-a", "bet-b"} {
		go func(index int, externalID string) {
			defer waitGroup.Done()
			<-start
			result, err := betVia(walletID, externalID, "80.00", cases, ctx)
			outcomes[index] = outcome{result: result, err: err}
		}(index, externalID)
	}
	close(start)
	waitGroup.Wait()

	var processed, rejected int
	for _, current := range outcomes {
		if current.err != nil {
			t.Fatalf("erro inesperado: %v", current.err)
		}
		switch current.result.Status {
		case wagering.Processed:
			processed++
		case wagering.Rejected:
			rejected++
			if current.result.FailureCode != wagering.FailureInsufficientFunds {
				t.Errorf("failureCode = %q", current.result.FailureCode)
			}
		default:
			t.Fatalf("status inesperado: %q", current.result.Status)
		}
	}

	if processed != 1 || rejected != 1 {
		t.Fatalf("resultados = %d processadas e %d rejeitadas", processed, rejected)
	}
	if balance := readWallet(t, ctx, walletID).Balance().String(); balance != "20.00" {
		t.Errorf("saldo = %s, esperado 20.00", balance)
	}
	if entries := countLedgerEntries(t, ctx, walletID); entries != 2 {
		t.Errorf("lançamentos = %d, esperados 2 (abertura e a aposta aceita)", entries)
	}

	// O reenvio não altera o resultado.
	for _, externalID := range []string{"bet-a", "bet-b"} {
		if _, err := betVia(walletID, externalID, "80.00", cases, ctx); err != nil {
			t.Fatalf("reenvio devolveu erro: %v", err)
		}
	}
	if balance := readWallet(t, ctx, walletID).Balance().String(); balance != "20.00" {
		t.Errorf("saldo após reenvios = %s, esperado 20.00", balance)
	}
}

// O replay devolve o saldo da época, mesmo depois de outras movimentações.
func TestReplayReturnsBalanceObservedAtProcessingTime(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	cases := newUseCases()
	walletID := openWalletVia(t, ctx, cases, "1000.00")

	first, err := betVia(walletID, "bet-1", "25.00", cases, ctx)
	if err != nil {
		t.Fatalf("primeira aposta devolveu erro: %v", err)
	}
	if _, err := betVia(walletID, "bet-2", "100.00", cases, ctx); err != nil {
		t.Fatalf("segunda aposta devolveu erro: %v", err)
	}

	replay, err := betVia(walletID, "bet-1", "25.00", cases, ctx)
	if err != nil {
		t.Fatalf("replay devolveu erro: %v", err)
	}

	if !replay.IdempotentReplay || !replay.TransactionID.Equal(first.TransactionID) {
		t.Errorf("replay = %+v", replay)
	}
	if replay.Balance.String() != "975.00" {
		t.Errorf("saldo do replay = %s, esperado 975.00", replay.Balance)
	}
	if current := readWallet(t, ctx, walletID).Balance().String(); current != "875.00" {
		t.Errorf("saldo atual = %s, esperado 875.00", current)
	}
}

func TestIdempotencyConflictsAreDetectedAgainstPostgres(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	cases := newUseCases()
	walletID := openWalletVia(t, ctx, cases, "1000.00")
	player := playerOf(ctx, walletID)

	if _, err := betVia(walletID, "bet-1", "25.00", cases, ctx); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}

	t.Run("mesma chave com conteúdo diferente", func(t *testing.T) {
		_, err := betVia(walletID, "bet-1", "99.00", cases, ctx)

		if !errors.Is(err, usecase.ErrIdempotencyConflict) {
			t.Errorf("erro = %v, esperado ErrIdempotencyConflict", err)
		}
	})

	t.Run("mesma operação com outra chave", func(t *testing.T) {
		_, err := cases.process.Execute(ctx, usecase.ProcessTransactionCommand{
			IdempotencyKey:        "provider-a:outra-chave",
			ProviderID:            "provider-a",
			ExternalTransactionID: "bet-1",
			PlayerID:              player,
			WalletID:              walletID.String(),
			RoundID:               "round-987",
			GameID:                "fortune-chimp",
			Kind:                  "BET",
			Money:                 usecase.MoneyInput{Amount: "25.00", Currency: "BRL"},
		})

		if !errors.Is(err, usecase.ErrOperationReapplied) {
			t.Errorf("erro = %v, esperado ErrOperationReapplied", err)
		}
	})
}

func TestReversalFlowAgainstPostgres(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	cases := newUseCases()
	walletID := openWalletVia(t, ctx, cases, "1000.00")
	player := playerOf(ctx, walletID)

	if _, err := betVia(walletID, "bet-1", "25.00", cases, ctx); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}

	reversal := func(externalID, kind, reference string) (usecase.TransactionResult, error) {
		return cases.process.Execute(ctx, usecase.ProcessTransactionCommand{
			IdempotencyKey:                 "provider-a:" + externalID,
			ProviderID:                     "provider-a",
			ExternalTransactionID:          externalID,
			PlayerID:                       player,
			WalletID:                       walletID.String(),
			RoundID:                        "round-987",
			GameID:                         "fortune-chimp",
			Kind:                           kind,
			Money:                          usecase.MoneyInput{Amount: "25.00", Currency: "BRL"},
			ReferenceExternalTransactionID: reference,
		})
	}

	refund, err := reversal("refund-1", "REFUND", "bet-1")
	if err != nil {
		t.Fatalf("estorno devolveu erro: %v", err)
	}
	if refund.Status != wagering.Processed || refund.Balance.String() != "1000.00" {
		t.Errorf("estorno = %s / %s", refund.Status, refund.Balance)
	}

	// A mesma aposta não pode ser devolvida de novo, nem por outro tipo.
	duplicated, err := reversal("rollback-1", "ROLLBACK", "bet-1")
	if err != nil {
		t.Fatalf("rollback devolveu erro: %v", err)
	}
	if duplicated.FailureCode != wagering.FailureDuplicateReversal {
		t.Errorf("failureCode = %q, esperado DUPLICATE_REVERSAL", duplicated.FailureCode)
	}

	// A reversão que chega antes da referência fica pendente.
	pending, err := reversal("rollback-2", "ROLLBACK", "aposta-que-nao-chegou")
	if err != nil {
		t.Fatalf("reversão fora de ordem devolveu erro: %v", err)
	}
	if pending.Status != wagering.PendingReference {
		t.Errorf("status = %q, esperado PENDING_REFERENCE", pending.Status)
	}
}

func TestReconciliationAgainstPostgres(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	cases := newUseCases()
	walletID := openWalletVia(t, ctx, cases, "1000.00")

	if _, err := betVia(walletID, "bet-1", "25.00", cases, ctx); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}

	result, err := cases.reconcile.Execute(ctx, walletID)
	if err != nil {
		t.Fatalf("reconciliação devolveu erro: %v", err)
	}

	if !result.Consistent {
		t.Errorf("esperava consistência: %+v", result)
	}
	if result.StoredBalance.String() != "975.00" || result.CalculatedBalance.String() != "975.00" {
		t.Errorf("saldos = %s / %s", result.StoredBalance, result.CalculatedBalance)
	}
	if result.CheckedEntries != 2 {
		t.Errorf("lançamentos verificados = %d, esperados 2", result.CheckedEntries)
	}

	// A reconciliação é somente leitura.
	if balance := readWallet(t, ctx, walletID).Balance().String(); balance != "975.00" {
		t.Errorf("saldo = %s, a reconciliação não deveria alterá-lo", balance)
	}
}

// Carteiras distintas seguem em paralelo mesmo com o caso de uso completo.
func TestDistinctWalletsProcessInParallel(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	cases := newUseCases()

	const wallets = 4
	ids := make([]shared.ID, wallets)
	for index := range ids {
		ids[index] = openWalletVia(t, ctx, cases, "100.00")
	}

	start := make(chan struct{})
	failures := make([]error, wallets)

	var waitGroup sync.WaitGroup
	waitGroup.Add(wallets)
	for index, walletID := range ids {
		go func(index int, walletID shared.ID) {
			defer waitGroup.Done()
			<-start
			_, failures[index] = betVia(walletID, "bet-"+walletID.String(), "80.00", cases, ctx)
		}(index, walletID)
	}

	began := time.Now()
	close(start)
	waitGroup.Wait()

	for index, err := range failures {
		if err != nil {
			t.Fatalf("carteira %d devolveu erro: %v", index, err)
		}
	}
	if elapsed := time.Since(began); elapsed > 5*time.Second {
		t.Errorf("as carteiras levaram %s: deveriam avançar em paralelo", elapsed)
	}

	for index, walletID := range ids {
		if balance := readWallet(t, ctx, walletID).Balance().String(); balance != "20.00" {
			t.Errorf("carteira %d = %s, esperado 20.00", index, balance)
		}
	}
}
