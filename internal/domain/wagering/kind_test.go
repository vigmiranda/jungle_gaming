package wagering_test

import (
	"errors"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

func brl(t *testing.T, amount string) money.Money {
	t.Helper()
	parsed, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("Parse(%q) devolveu erro: %v", amount, err)
	}
	return parsed
}

func TestParseExternalKindAcceptsTheFiveExternalTypes(t *testing.T) {
	for _, raw := range []string{"BET", "WIN", "LOSS", "REFUND", "ROLLBACK"} {
		t.Run(raw, func(t *testing.T) {
			parsed, err := wagering.ParseExternalKind(raw)
			if err != nil {
				t.Fatalf("ParseExternalKind devolveu erro: %v", err)
			}
			if parsed.String() != raw {
				t.Errorf("String = %q, esperado %q", parsed.String(), raw)
			}
		})
	}
}

// OPENING é reservado à abertura interna: aceitar por HTTP ou SQS permitiria a
// um provedor criar crédito do nada.
func TestParseExternalKindRejectsOpening(t *testing.T) {
	_, err := wagering.ParseExternalKind("OPENING")

	if !errors.Is(err, wagering.ErrOpeningNotAllowed) {
		t.Errorf("erro = %v, esperado ErrOpeningNotAllowed", err)
	}
}

func TestParseExternalKindRejectsUnknownTypes(t *testing.T) {
	for _, raw := range []string{"", "bet", "DEPOSIT", "CASHOUT"} {
		t.Run(raw, func(t *testing.T) {
			_, err := wagering.ParseExternalKind(raw)

			if !errors.Is(err, wagering.ErrUnknownKind) {
				t.Errorf("erro = %v, esperado ErrUnknownKind", err)
			}
		})
	}
}

func TestParseKindAcceptsInternalOrigin(t *testing.T) {
	parsed, err := wagering.ParseKind("OPENING")
	if err != nil {
		t.Fatalf("ParseKind devolveu erro: %v", err)
	}
	if parsed != wagering.Opening {
		t.Errorf("ParseKind = %q, esperado OPENING", parsed)
	}

	if _, err := wagering.ParseKind("DEPOSIT"); !errors.Is(err, wagering.ErrUnknownKind) {
		t.Errorf("erro = %v, esperado ErrUnknownKind", err)
	}
}

func TestKindClassification(t *testing.T) {
	tests := []struct {
		kind        wagering.Kind
		isReversal  bool
		movesAmount bool
	}{
		{wagering.Opening, false, true},
		{wagering.Bet, false, true},
		{wagering.Win, false, true},
		{wagering.Loss, false, false},
		{wagering.Refund, true, true},
		{wagering.Rollback, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			if tt.kind.IsReversal() != tt.isReversal {
				t.Errorf("IsReversal = %v, esperado %v", tt.kind.IsReversal(), tt.isReversal)
			}
			if tt.kind.MovesBalance() != tt.movesAmount {
				t.Errorf("MovesBalance = %v, esperado %v", tt.kind.MovesBalance(), tt.movesAmount)
			}
		})
	}
}

// LOSS exige exatamente zero; os demais exigem valor maior que zero.
func TestValidateAmountAppliesZeroPolicyPerKind(t *testing.T) {
	positive := brl(t, "25.00")
	zero := brl(t, "0.00")
	negative := money.FromMinorUnits(-2500, money.BRL)

	tests := []struct {
		name        string
		kind        wagering.Kind
		amount      money.Money
		wantAccepts bool
	}{
		{"BET positivo", wagering.Bet, positive, true},
		{"BET zero", wagering.Bet, zero, false},
		{"BET negativo", wagering.Bet, negative, false},
		{"WIN positivo", wagering.Win, positive, true},
		{"WIN zero", wagering.Win, zero, false},
		{"LOSS zero", wagering.Loss, zero, true},
		{"LOSS positivo", wagering.Loss, positive, false},
		{"LOSS negativo", wagering.Loss, negative, false},
		{"REFUND positivo", wagering.Refund, positive, true},
		{"REFUND zero", wagering.Refund, zero, false},
		{"ROLLBACK positivo", wagering.Rollback, positive, true},
		{"ROLLBACK negativo", wagering.Rollback, negative, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.kind.ValidateAmount(tt.amount)

			if tt.wantAccepts && err != nil {
				t.Errorf("esperava aceitar %s, erro = %v", tt.amount, err)
			}
			if !tt.wantAccepts && !errors.Is(err, wagering.ErrInvalidAmountForKind) {
				t.Errorf("erro = %v, esperado ErrInvalidAmountForKind", err)
			}
		})
	}
}

func TestValidateAmountRejectsUninitializedMoney(t *testing.T) {
	var uninitialized money.Money

	if err := wagering.Bet.ValidateAmount(uninitialized); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("erro = %v, esperado ErrUninitialized", err)
	}
	if err := wagering.Loss.ValidateAmount(uninitialized); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("erro = %v, esperado ErrUninitialized", err)
	}
}

func TestValidateReferencePerKind(t *testing.T) {
	tests := []struct {
		name      string
		kind      wagering.Kind
		reference string
		wantCode  error
	}{
		{"REFUND com referência", wagering.Refund, "transaction-123", nil},
		{"ROLLBACK com referência", wagering.Rollback, "transaction-123", nil},
		{"REFUND sem referência", wagering.Refund, "", wagering.ErrReferenceRequired},
		{"ROLLBACK sem referência", wagering.Rollback, "", wagering.ErrReferenceRequired},
		{"WIN pode referenciar a aposta", wagering.Win, "transaction-123", nil},
		{"WIN sem referência", wagering.Win, "", nil},
		{"BET sem referência", wagering.Bet, "", nil},
		{"BET com referência", wagering.Bet, "transaction-123", wagering.ErrReferenceNotAllowed},
		{"LOSS com referência", wagering.Loss, "transaction-123", wagering.ErrReferenceNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.kind.ValidateReference(tt.reference)

			if tt.wantCode == nil {
				if err != nil {
					t.Errorf("esperava aceitar, erro = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}

func TestFailureCodeString(t *testing.T) {
	if got := wagering.FailureInsufficientFunds.String(); got != "INSUFFICIENT_FUNDS" {
		t.Errorf("String = %q", got)
	}
	// A reversão sem saldo precisa ser distinguível de uma aposta sem saldo.
	if wagering.FailureReversalExceedsBalance == wagering.FailureInsufficientFunds {
		t.Error("os códigos de saldo insuficiente deveriam ser distintos")
	}
}
