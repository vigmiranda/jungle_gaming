package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

// setupWallet abre uma carteira e devolve o identificador.
func setupWallet(t *testing.T, f *fixture, balance string) string {
	t.Helper()

	result, err := f.open.Execute(context.Background(), openWalletCommand(balance))
	if err != nil {
		t.Fatalf("abertura devolveu erro: %v", err)
	}
	return result.Wallet.ID().String()
}

func betCommand(walletID, externalID, amount string) usecase.ProcessTransactionCommand {
	return usecase.ProcessTransactionCommand{
		IdempotencyKey:        "provider-a:" + externalID,
		ProviderID:            "provider-a",
		ExternalTransactionID: externalID,
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  "BET",
		Money:                 usecase.MoneyInput{Amount: amount, Currency: "BRL"},
	}
}

func TestProcessBetDebitsWallet(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	result, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "25.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.Status != wagering.Processed {
		t.Errorf("status = %q, esperado PROCESSED", result.Status)
	}
	if !result.HasBalance || result.Balance.String() != "975.00" {
		t.Errorf("saldo = %s", result.Balance)
	}
	if result.IdempotentReplay {
		t.Error("a primeira execução não é replay")
	}
	if entries := len(f.unitOfWork.state.ledger); entries != 2 {
		t.Errorf("lançamentos = %d, esperados 2 (abertura e aposta)", entries)
	}
	assertOutboxContains(t, f.unitOfWork.state.outbox,
		usecase.EventWagerTransactionProcessed,
		usecase.EventWalletBalanceChanged,
	)
	// Abertura + aposta: 2 eventos cada.
	if total := len(f.unitOfWork.state.outbox); total != 4 {
		t.Errorf("outbox = %d eventos, esperados 4", total)
	}
}

func TestProcessWinCreditsWallet(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")

	command := betCommand(walletID, "transaction-win", "40.00")
	command.Kind = "WIN"

	result, err := f.process.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.Balance.String() != "140.00" {
		t.Errorf("saldo = %s, esperado 140.00", result.Balance)
	}
}

// LOSS conclui sem movimentar saldo: sem lançamento e sem bump de versão.
func TestProcessLossDoesNotMoveBalance(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "100.00")
	entriesBefore := len(f.unitOfWork.state.ledger)

	command := betCommand(walletID, "transaction-loss", "0.00")
	command.Kind = "LOSS"

	result, err := f.process.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.Status != wagering.Processed || result.Balance.String() != "100.00" {
		t.Errorf("resultado = %s / %s", result.Status, result.Balance)
	}
	if len(f.unitOfWork.state.ledger) != entriesBefore {
		t.Error("LOSS não deveria produzir lançamento")
	}

	stored := f.unitOfWork.state.wallets[walletID].wallet
	if stored.Version() != 1 {
		t.Errorf("versão = %d, esperada 1 (LOSS não incrementa)", stored.Version())
	}

	if got := countOutboxType(f.unitOfWork.state.outbox, usecase.EventWagerTransactionProcessed); got != 2 {
		t.Errorf("Processed na outbox = %d, esperado 2 (abertura + LOSS)", got)
	}
	if got := countOutboxType(f.unitOfWork.state.outbox, usecase.EventWalletBalanceChanged); got != 1 {
		t.Errorf("BalanceChanged na outbox = %d, esperado 1 (só abertura)", got)
	}
}

func TestProcessBetWithoutFundsIsRejectedAndPersisted(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "10.00")

	result, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-2", "80.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.Status != wagering.Rejected {
		t.Errorf("status = %q, esperado REJECTED", result.Status)
	}
	if result.FailureCode != wagering.FailureInsufficientFunds {
		t.Errorf("failureCode = %q", result.FailureCode)
	}
	if result.HasBalance {
		t.Error("uma rejeição não devolve saldo")
	}

	// A rejeição é terminal e persistida: o reenvio devolve a mesma recusa.
	replay, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-2", "80.00"))
	if err != nil {
		t.Fatalf("replay devolveu erro: %v", err)
	}
	if !replay.IdempotentReplay || replay.FailureCode != wagering.FailureInsufficientFunds {
		t.Errorf("replay = %+v", replay)
	}
	if got := countOutboxType(f.unitOfWork.state.outbox, usecase.EventWagerTransactionRejected); got != 1 {
		t.Errorf("Rejected na outbox = %d, esperado 1", got)
	}
}

// O replay devolve o saldo observado no processamento original, mesmo depois de
// a carteira ter se movimentado (ADR-014).
func TestReplayReturnsBalanceFromOriginalProcessing(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	first, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "25.00"))
	if err != nil {
		t.Fatalf("primeira aposta devolveu erro: %v", err)
	}

	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-2", "100.00")); err != nil {
		t.Fatalf("segunda aposta devolveu erro: %v", err)
	}

	replay, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "25.00"))
	if err != nil {
		t.Fatalf("replay devolveu erro: %v", err)
	}

	if !replay.IdempotentReplay {
		t.Error("esperava idempotentReplay verdadeiro")
	}
	if !replay.TransactionID.Equal(first.TransactionID) {
		t.Errorf("transactionId = %s, esperado %s", replay.TransactionID, first.TransactionID)
	}
	if replay.Balance.String() != "975.00" {
		t.Errorf("saldo do replay = %s, esperado 975.00 (o saldo da época)", replay.Balance)
	}
	// O saldo atual é outro: o replay não pode devolvê-lo.
	if current := f.unitOfWork.state.wallets[walletID].wallet.Balance().String(); current != "875.00" {
		t.Errorf("saldo atual = %s, esperado 875.00", current)
	}
}

func TestSameKeyWithDifferentPayloadIsConflict(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "25.00")); err != nil {
		t.Fatalf("primeira aposta devolveu erro: %v", err)
	}

	divergent := betCommand(walletID, "transaction-1", "99.00")

	_, err := f.process.Execute(context.Background(), divergent)

	if !errors.Is(err, usecase.ErrIdempotencyConflict) {
		t.Errorf("erro = %v, esperado ErrIdempotencyConflict", err)
	}
}

// A mesma operação financeira não pode ser reaplicada sob outra chave.
func TestSameOperationWithDifferentKeyIsConflict(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "25.00")); err != nil {
		t.Fatalf("primeira aposta devolveu erro: %v", err)
	}

	reused := betCommand(walletID, "transaction-1", "25.00")
	reused.IdempotencyKey = "provider-a:outra-chave"

	_, err := f.process.Execute(context.Background(), reused)

	if !errors.Is(err, usecase.ErrOperationReapplied) {
		t.Errorf("erro = %v, esperado ErrOperationReapplied", err)
	}
}

func TestProcessRejectsInvalidCommands(t *testing.T) {
	walletID := "0192f291-27dd-7d3f-8071-5f8685deef37"

	tests := []struct {
		name     string
		mutate   func(*usecase.ProcessTransactionCommand)
		wantCode error
	}{
		{"sem chave de idempotência", func(c *usecase.ProcessTransactionCommand) { c.IdempotencyKey = "" }, usecase.ErrInvalidInput},
		{"sem provedor", func(c *usecase.ProcessTransactionCommand) { c.ProviderID = "" }, usecase.ErrInvalidInput},
		{"sem id externo", func(c *usecase.ProcessTransactionCommand) { c.ExternalTransactionID = "" }, usecase.ErrInvalidInput},
		{"carteira inválida", func(c *usecase.ProcessTransactionCommand) { c.WalletID = "carteira-1" }, usecase.ErrInvalidInput},
		{"jogador inválido", func(c *usecase.ProcessTransactionCommand) { c.PlayerID = "jogador-1" }, usecase.ErrInvalidInput},
		{"tipo desconhecido", func(c *usecase.ProcessTransactionCommand) { c.Kind = "DEPOSIT" }, wagering.ErrUnknownKind},
		{"abertura por canal externo", func(c *usecase.ProcessTransactionCommand) { c.Kind = "OPENING" }, wagering.ErrOpeningNotAllowed},
		{"valor não canônico", func(c *usecase.ProcessTransactionCommand) { c.Money.Amount = "25.0" }, money.ErrInvalidAmount},
		{"moeda inválida", func(c *usecase.ProcessTransactionCommand) { c.Money.Currency = "brl" }, money.ErrInvalidCurrency},
		{"BET com valor zero", func(c *usecase.ProcessTransactionCommand) { c.Money.Amount = "0.00" }, wagering.ErrInvalidAmountForKind},
		{"LOSS com valor positivo", func(c *usecase.ProcessTransactionCommand) { c.Kind = "LOSS" }, wagering.ErrInvalidAmountForKind},
		{"BET com referência", func(c *usecase.ProcessTransactionCommand) { c.ReferenceExternalTransactionID = "x" }, wagering.ErrReferenceNotAllowed},
		{"REFUND sem referência", func(c *usecase.ProcessTransactionCommand) { c.Kind = "REFUND" }, wagering.ErrReferenceRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			command := betCommand(walletID, "transaction-1", "25.00")
			tt.mutate(&command)

			_, err := f.process.Execute(context.Background(), command)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}

// Campos obrigatórios da operação externa que o domínio exige mas que não têm
// forma própria de validação no comando.
func TestProcessRejectsMissingRoundOrGame(t *testing.T) {
	for _, field := range []string{"roundId", "gameId"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture()
			walletID := setupWallet(t, f, "1000.00")

			command := betCommand(walletID, "transaction-1", "25.00")
			if field == "roundId" {
				command.RoundID = ""
			} else {
				command.GameID = ""
			}

			_, err := f.process.Execute(context.Background(), command)

			if !errors.Is(err, wagering.ErrMissingField) {
				t.Errorf("erro = %v, esperado ErrMissingField", err)
			}
		})
	}
}

func TestProcessRejectsUnknownWallet(t *testing.T) {
	f := newFixture()

	_, err := f.process.Execute(context.Background(),
		betCommand("0192f291-27dd-7d3f-8071-5f8685deef37", "transaction-1", "25.00"))

	if err == nil {
		t.Fatal("esperava erro para carteira inexistente")
	}
}

func TestProcessRejectsWalletOfAnotherPlayer(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	command := betCommand(walletID, "transaction-1", "25.00")
	command.PlayerID = "0192f2b0-0000-7000-8000-000000000009"

	result, err := f.process.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.FailureCode != wagering.FailureWalletPlayerMismatch {
		t.Errorf("failureCode = %q, esperado WALLET_PLAYER_MISMATCH", result.FailureCode)
	}
}

func TestProcessRejectsCurrencyDifferentFromWallet(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	command := betCommand(walletID, "transaction-1", "25.00")
	command.Money.Currency = "USD"

	result, err := f.process.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.FailureCode != wagering.FailureCurrencyMismatch {
		t.Errorf("failureCode = %q, esperado CURRENCY_MISMATCH", result.FailureCode)
	}
}

func TestProcessPropagatesInfrastructureFailures(t *testing.T) {
	failure := errors.New("falha simulada")

	tests := []struct {
		name  string
		setup func(*fixture)
	}{
		{"lock da carteira", func(f *fixture) { f.unitOfWork.state.walletLockErr = failure }},
		{"consulta por chave", func(f *fixture) { f.unitOfWork.state.findByKeyErr = failure }},
		{"consulta por id externo", func(f *fixture) { f.unitOfWork.state.findExternalErr = failure }},
		{"gravação da transação", func(f *fixture) { f.unitOfWork.state.transactionErr = failure }},
		{"gravação do lançamento", func(f *fixture) { f.unitOfWork.state.ledgerAppendErr = failure }},
		{"atualização do saldo", func(f *fixture) { f.unitOfWork.state.walletUpdateErr = failure }},
		{"gravação da outbox", func(f *fixture) { f.unitOfWork.state.outboxAppendErr = failure }},
		{"início da transação", func(f *fixture) { f.unitOfWork.beginErr = failure }},
		{"geração de identificador", func(f *fixture) { f.ids.failAfter = f.ids.calls + 1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			walletID := setupWallet(t, f, "1000.00")
			tt.setup(f)

			if _, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "25.00")); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

// As gravações de rejeição e de pendência também precisam propagar falha: uma
// recusa que não chega ao banco não pode ser reportada como registrada.
func TestProcessPropagatesFailureWhenPersistingTerminalStates(t *testing.T) {
	failure := errors.New("falha simulada")

	t.Run("rejeição", func(t *testing.T) {
		f := newFixture()
		walletID := setupWallet(t, f, "10.00")
		f.unitOfWork.state.transactionErr = failure

		_, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "80.00"))

		if !errors.Is(err, failure) {
			t.Errorf("erro = %v, esperado a falha simulada", err)
		}
	})

	t.Run("rejeição na outbox", func(t *testing.T) {
		f := newFixture()
		walletID := setupWallet(t, f, "10.00")
		f.unitOfWork.state.outboxAppendErr = failure

		_, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "80.00"))

		if !errors.Is(err, failure) {
			t.Errorf("erro = %v, esperado a falha simulada", err)
		}
	})

	t.Run("pendência de referência", func(t *testing.T) {
		f := newFixture()
		walletID := setupWallet(t, f, "1000.00")
		f.unitOfWork.state.transactionErr = failure

		command := betCommand(walletID, "rollback-1", "25.00")
		command.Kind = "ROLLBACK"
		command.ReferenceExternalTransactionID = "aposta-que-nao-chegou"

		_, err := f.process.Execute(context.Background(), command)

		if !errors.Is(err, failure) {
			t.Errorf("erro = %v, esperado a falha simulada", err)
		}
	})

	t.Run("pendência na outbox", func(t *testing.T) {
		f := newFixture()
		walletID := setupWallet(t, f, "1000.00")
		f.unitOfWork.state.outboxAppendErr = failure

		command := betCommand(walletID, "rollback-1", "25.00")
		command.Kind = "ROLLBACK"
		command.ReferenceExternalTransactionID = "aposta-que-nao-chegou"

		_, err := f.process.Execute(context.Background(), command)

		if !errors.Is(err, failure) {
			t.Errorf("erro = %v, esperado a falha simulada", err)
		}
	})
}

// Estouro na movimentação não é rejeição de negócio: o erro sobe, em vez de
// virar um failureCode que sugeriria recusa por regra.
func TestProcessPropagatesOverflowOnCredit(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "92233720368547758.07")

	command := betCommand(walletID, "win-1", "0.01")
	command.Kind = "WIN"

	_, err := f.process.Execute(context.Background(), command)

	if !errors.Is(err, money.ErrOverflow) {
		t.Errorf("erro = %v, esperado ErrOverflow", err)
	}
}

func TestProcessPropagatesFailureGeneratingLedgerID(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")
	// A aposta consome o próximo id para a transação e o seguinte para o lançamento.
	f.ids.failAfter = f.ids.calls + 2

	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "25.00")); err == nil {
		t.Error("esperava erro")
	}
}

func TestProcessRollsBackOnFailureAfterWrites(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")
	f.unitOfWork.state.walletUpdateErr = errors.New("falha simulada")

	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "transaction-1", "25.00")); err == nil {
		t.Fatal("esperava erro")
	}

	if balance := f.unitOfWork.state.wallets[walletID].wallet.Balance().String(); balance != "1000.00" {
		t.Errorf("saldo = %s, esperado 1000.00 após o rollback", balance)
	}
	if len(f.unitOfWork.state.transactions) != 1 {
		t.Error("apenas a abertura deveria estar persistida")
	}
}
