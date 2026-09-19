package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

func reversalCommand(walletID, externalID, kind, reference, amount string) usecase.ProcessTransactionCommand {
	command := betCommand(walletID, externalID, amount)
	command.Kind = kind
	command.ReferenceExternalTransactionID = reference
	return command
}

func TestRefundCreditsBackTheReferencedBet(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "25.00")); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}

	result, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "refund-1", "REFUND", "bet-1", "25.00"))
	if err != nil {
		t.Fatalf("estorno devolveu erro: %v", err)
	}

	if result.Status != wagering.Processed {
		t.Errorf("status = %q, esperado PROCESSED", result.Status)
	}
	if result.Balance.String() != "1000.00" {
		t.Errorf("saldo = %s, esperado 1000.00 (aposta devolvida)", result.Balance)
	}
}

func TestRollbackReversesTheOriginalDirection(t *testing.T) {
	tests := []struct {
		name         string
		originalKind string
		amount       string
		afterOrigin  string
		afterRollack string
	}{
		{"desfaz aposta creditando", "BET", "25.00", "975.00", "1000.00"},
		{"desfaz prêmio debitando", "WIN", "25.00", "1025.00", "1000.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			walletID := setupWallet(t, f, "1000.00")

			original := betCommand(walletID, "original-1", tt.amount)
			original.Kind = tt.originalKind
			result, err := f.process.Execute(context.Background(), original)
			if err != nil {
				t.Fatalf("operação original devolveu erro: %v", err)
			}
			if result.Balance.String() != tt.afterOrigin {
				t.Fatalf("saldo após a original = %s, esperado %s", result.Balance, tt.afterOrigin)
			}

			rollback, err := f.process.Execute(context.Background(),
				reversalCommand(walletID, "rollback-1", "ROLLBACK", "original-1", tt.amount))
			if err != nil {
				t.Fatalf("rollback devolveu erro: %v", err)
			}
			if rollback.Balance.String() != tt.afterRollack {
				t.Errorf("saldo após o rollback = %s, esperado %s", rollback.Balance, tt.afterRollack)
			}
		})
	}
}

// A reversão que chega antes da operação referenciada fica pendente, e é o
// worker de referências que continua depois.
func TestReversalBeforeReferenceBecomesPending(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	result, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-1", "ROLLBACK", "aposta-que-nao-chegou", "25.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.Status != wagering.PendingReference {
		t.Errorf("status = %q, esperado PENDING_REFERENCE", result.Status)
	}
	if result.HasBalance {
		t.Error("uma pendência não devolve saldo")
	}
	if result.FailureCode != "" {
		t.Errorf("failureCode = %q, esperado vazio", result.FailureCode)
	}
}

// Referência ainda em voo: esperar é o que sustenta a entrega fora de ordem.
func TestReversalWaitsWhileReferenceIsNotTerminal(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	// A primeira reversão fica pendente e serve de referência não terminal.
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "refund-1", "REFUND", "bet-inexistente", "25.00")); err != nil {
		t.Fatalf("primeira reversão devolveu erro: %v", err)
	}

	result, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-1", "ROLLBACK", "refund-1", "25.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.Status != wagering.PendingReference {
		t.Errorf("status = %q, esperado PENDING_REFERENCE", result.Status)
	}
}

func TestReversalRejectsIneligibleReference(t *testing.T) {
	tests := []struct {
		name          string
		referenceKind string
		reversalKind  string
	}{
		{"REFUND só desfaz aposta", "WIN", "REFUND"},
		{"REFUND não desfaz perda", "LOSS", "REFUND"},
		{"ROLLBACK não desfaz perda", "LOSS", "ROLLBACK"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			walletID := setupWallet(t, f, "1000.00")

			amount := "25.00"
			if tt.referenceKind == "LOSS" {
				amount = "0.00"
			}
			reference := betCommand(walletID, "reference-1", amount)
			reference.Kind = tt.referenceKind
			if _, err := f.process.Execute(context.Background(), reference); err != nil {
				t.Fatalf("operação de referência devolveu erro: %v", err)
			}

			result, err := f.process.Execute(context.Background(),
				reversalCommand(walletID, "reversal-1", tt.reversalKind, "reference-1", "25.00"))
			if err != nil {
				t.Fatalf("Execute devolveu erro: %v", err)
			}

			if result.FailureCode != wagering.FailureReferenceNotProcessed {
				t.Errorf("failureCode = %q, esperado REFERENCE_NOT_PROCESSED", result.FailureCode)
			}
		})
	}
}

func TestReversalRejectsTerminalButUnsuccessfulReference(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "10.00")

	// A aposta é recusada por saldo: terminal, porém não elegível a reversão.
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "80.00")); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}

	result, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "refund-1", "REFUND", "bet-1", "80.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.FailureCode != wagering.FailureReferenceNotProcessed {
		t.Errorf("failureCode = %q, esperado REFERENCE_NOT_PROCESSED", result.FailureCode)
	}
}

// A operação e sua referência precisam concordar em jogador, carteira, rodada
// e valor. Reversão parcial não faz parte do desafio.
func TestReversalRejectsDivergentReference(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*usecase.ProcessTransactionCommand)
	}{
		{"valor diferente", func(c *usecase.ProcessTransactionCommand) { c.Money.Amount = "10.00" }},
		{"rodada diferente", func(c *usecase.ProcessTransactionCommand) { c.RoundID = "round-outra" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			walletID := setupWallet(t, f, "1000.00")
			if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "25.00")); err != nil {
				t.Fatalf("aposta devolveu erro: %v", err)
			}

			command := reversalCommand(walletID, "refund-1", "REFUND", "bet-1", "25.00")
			tt.mutate(&command)

			result, err := f.process.Execute(context.Background(), command)
			if err != nil {
				t.Fatalf("Execute devolveu erro: %v", err)
			}

			if result.FailureCode != wagering.FailureReferenceMismatch {
				t.Errorf("failureCode = %q, esperado REFERENCE_MISMATCH", result.FailureCode)
			}
		})
	}
}

// Impede devolver o mesmo débito duas vezes.
func TestSecondSuccessfulReversalOfSameKindIsRejected(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "25.00")); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "refund-1", "REFUND", "bet-1", "25.00")); err != nil {
		t.Fatalf("primeiro estorno devolveu erro: %v", err)
	}

	result, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "refund-2", "REFUND", "bet-1", "25.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.FailureCode != wagering.FailureDuplicateReversal {
		t.Errorf("failureCode = %q, esperado DUPLICATE_REVERSAL", result.FailureCode)
	}
}

// A combinação de REFUND e ROLLBACK sobre a mesma aposta devolveria o mesmo
// débito duas vezes, então a segunda reversão é recusada mesmo sendo de outro
// tipo. O ROLLBACK do próprio estorno continua válido: desfaz o crédito.
func TestRefundAndRollbackCannotReturnTheSameDebitTwice(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "25.00")
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "25.00")); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}
	if _, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "refund-1", "REFUND", "bet-1", "25.00")); err != nil {
		t.Fatalf("estorno devolveu erro: %v", err)
	}

	duplicated, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-1", "ROLLBACK", "bet-1", "25.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}
	if duplicated.FailureCode != wagering.FailureDuplicateReversal {
		t.Errorf("failureCode = %q, esperado DUPLICATE_REVERSAL", duplicated.FailureCode)
	}

	// O saldo permanece com a aposta devolvida uma única vez.
	if balance := f.unitOfWork.state.wallets[walletID].wallet.Balance().String(); balance != "25.00" {
		t.Errorf("saldo = %s, esperado 25.00", balance)
	}

	rollback, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-2", "ROLLBACK", "refund-1", "25.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}
	if rollback.Status != wagering.Processed || rollback.Balance.String() != "0.00" {
		t.Errorf("rollback do estorno = %s / %s", rollback.Status, rollback.Balance)
	}
}

// A reversão que deixaria o saldo negativo é recusada com código próprio,
// distinto do usado para aposta sem saldo.
func TestReversalThatWouldDrainTheWalletIsRejected(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "0.00")

	win := betCommand(walletID, "win-1", "25.00")
	win.Kind = "WIN"
	if _, err := f.process.Execute(context.Background(), win); err != nil {
		t.Fatalf("prêmio devolveu erro: %v", err)
	}
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "25.00")); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}

	// Desfazer o prêmio exigiria debitar 25.00 de um saldo zerado.
	result, err := f.process.Execute(context.Background(),
		reversalCommand(walletID, "rollback-1", "ROLLBACK", "win-1", "25.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.FailureCode != wagering.FailureReversalExceedsBalance {
		t.Errorf("failureCode = %q, esperado REVERSAL_EXCEEDS_BALANCE", result.FailureCode)
	}
	if result.FailureCode == wagering.FailureInsufficientFunds {
		t.Error("o código deve ser distinto do de aposta sem saldo")
	}
}

func TestReversalPropagatesInfrastructureFailures(t *testing.T) {
	failure := errors.New("falha simulada")

	tests := []struct {
		name  string
		setup func(*fixture)
	}{
		{"consulta da referência", func(f *fixture) { f.unitOfWork.state.findExternalErr = failure }},
		{"consulta de reversão existente", func(f *fixture) { f.unitOfWork.state.reversalCheckErr = failure }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			walletID := setupWallet(t, f, "1000.00")
			if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "25.00")); err != nil {
				t.Fatalf("aposta devolveu erro: %v", err)
			}
			tt.setup(f)

			_, err := f.process.Execute(context.Background(),
				reversalCommand(walletID, "refund-1", "REFUND", "bet-1", "25.00"))

			if !errors.Is(err, failure) {
				t.Errorf("erro = %v, esperado a falha simulada", err)
			}
		})
	}
}
