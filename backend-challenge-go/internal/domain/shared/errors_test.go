package shared_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

func TestConstructorsCarryKind(t *testing.T) {
	tests := []struct {
		name string
		err  *shared.Error
		kind shared.Kind
	}{
		{"validação", shared.Validation("INVALID_AMOUNT", "valor inválido"), shared.KindValidation},
		{"rejeição", shared.Rejection("INSUFFICIENT_FUNDS", "saldo insuficiente"), shared.KindRejection},
		{"conflito", shared.Conflict("WALLET_ALREADY_EXISTS", "carteira já existe"), shared.KindConflict},
		{"invariante", shared.Invariant("ILLEGAL_TRANSITION", "transição ilegal"), shared.KindInvariant},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Kind != tt.kind {
				t.Errorf("Kind = %q, esperado %q", tt.err.Kind, tt.kind)
			}
			if tt.err.Error() == "" {
				t.Error("Error() não deve ser vazio")
			}
		})
	}
}

func TestErrorFormatsCodeAndMessage(t *testing.T) {
	err := shared.Validation("INVALID_AMOUNT", "valor inválido")

	if got, want := err.Error(), "INVALID_AMOUNT: valor inválido"; got != want {
		t.Errorf("Error() = %q, esperado %q", got, want)
	}
}

func TestErrorIncludesCauseWhenPresent(t *testing.T) {
	cause := errors.New("estouro de inteiro")
	err := shared.Validation("INVALID_AMOUNT", "valor inválido").WithCause(cause)

	if got, want := err.Error(), "INVALID_AMOUNT: valor inválido: estouro de inteiro"; got != want {
		t.Errorf("Error() = %q, esperado %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Error("a causa deveria ser alcançável por errors.Is")
	}
}

// A comparação por código é o que permite mapear failureCode estável no
// contrato externo sem depender do texto da mensagem.
func TestIsComparesByCodeIgnoringMessage(t *testing.T) {
	sentinel := shared.Rejection("INSUFFICIENT_FUNDS", "saldo insuficiente")
	concrete := shared.Rejection("INSUFFICIENT_FUNDS", "saldo insuficiente").
		Messagef("saldo %s é menor que %s", "10.00", "80.00")

	if !errors.Is(concrete, sentinel) {
		t.Error("erros com o mesmo código deveriam ser equivalentes")
	}
	if errors.Is(concrete, shared.Rejection("CURRENCY_MISMATCH", "moeda incompatível")) {
		t.Error("erros com códigos diferentes não deveriam ser equivalentes")
	}
	if errors.Is(concrete, errors.New("INSUFFICIENT_FUNDS")) {
		t.Error("um erro genérico não deveria casar com um erro de domínio")
	}
}

func TestMessagefKeepsCodeAndKind(t *testing.T) {
	original := shared.Rejection("INSUFFICIENT_FUNDS", "saldo insuficiente")
	detailed := original.Messagef("faltam %s", "55.00")

	if detailed.Code != original.Code || detailed.Kind != original.Kind {
		t.Errorf("detalhar a mensagem não deveria mudar código ou classe: %+v", detailed)
	}
	if detailed.Message != "faltam 55.00" {
		t.Errorf("Message = %q", detailed.Message)
	}
	if original.Message != "saldo insuficiente" {
		t.Errorf("o erro original foi mutado: %q", original.Message)
	}
}

func TestWithCauseDoesNotMutateOriginal(t *testing.T) {
	original := shared.Validation("INVALID_AMOUNT", "valor inválido")
	derived := original.WithCause(errors.New("causa"))

	if errors.Unwrap(original) != nil {
		t.Error("o erro original não deveria ganhar causa")
	}
	if errors.Unwrap(derived) == nil {
		t.Error("o erro derivado deveria expor a causa")
	}
}

func TestErrorIsUsableWithErrorsAs(t *testing.T) {
	wrapped := fmt.Errorf("ao processar operação: %w",
		shared.Rejection("INSUFFICIENT_FUNDS", "saldo insuficiente"))

	var domainErr *shared.Error
	if !errors.As(wrapped, &domainErr) {
		t.Fatal("esperava recuperar o erro de domínio com errors.As")
	}
	if domainErr.Code != "INSUFFICIENT_FUNDS" {
		t.Errorf("Code = %q", domainErr.Code)
	}
}
