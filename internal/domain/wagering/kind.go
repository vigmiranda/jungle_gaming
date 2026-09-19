package wagering

import (
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// Kind é o tipo da operação.
type Kind string

// Tipos suportados. `Opening` é exclusivo da abertura interna de carteira; os
// demais chegam por HTTP ou SQS.
const (
	Opening  Kind = "OPENING"
	Bet      Kind = "BET"
	Win      Kind = "WIN"
	Loss     Kind = "LOSS"
	Refund   Kind = "REFUND"
	Rollback Kind = "ROLLBACK"
)

// Erros de tipo de operação.
var (
	// ErrUnknownKind cobre tipos fora da lista suportada.
	ErrUnknownKind = shared.Validation("UNKNOWN_KIND", "tipo de operação desconhecido")
	// ErrOpeningNotAllowed cobre `OPENING` recebido por canal externo.
	ErrOpeningNotAllowed = shared.Rejection("OPENING_NOT_ALLOWED", "abertura não pode ser solicitada por canal externo")
	// ErrInvalidAmountForKind cobre valor incompatível com a política do tipo.
	ErrInvalidAmountForKind = shared.Validation("INVALID_AMOUNT", "valor incompatível com o tipo da operação")
	// ErrReferenceRequired cobre reversão sem a referência obrigatória.
	ErrReferenceRequired = shared.Validation("REFERENCE_REQUIRED", "referência externa é obrigatória para reversões")
	// ErrReferenceNotAllowed cobre referência informada em tipo que não a aceita.
	ErrReferenceNotAllowed = shared.Validation("REFERENCE_NOT_ALLOWED", "tipo de operação não aceita referência")
)

// ParseExternalKind converte o tipo recebido de um provedor.
//
// `OPENING` é recusado aqui: a abertura é interna e não pode ser induzida por
// HTTP ou SQS.
func ParseExternalKind(raw string) (Kind, error) {
	kind := Kind(raw)
	switch kind {
	case Bet, Win, Loss, Refund, Rollback:
		return kind, nil
	case Opening:
		return "", ErrOpeningNotAllowed.Messagef("tipo %q é reservado à abertura interna", raw)
	default:
		return "", ErrUnknownKind.Messagef("tipo %q não é suportado", raw)
	}
}

// ParseKind converte o tipo persistido, incluindo a origem interna.
func ParseKind(raw string) (Kind, error) {
	if Kind(raw) == Opening {
		return Opening, nil
	}
	return ParseExternalKind(raw)
}

// String devolve a representação textual.
func (k Kind) String() string { return string(k) }

// IsReversal indica os tipos que desfazem uma operação anterior.
func (k Kind) IsReversal() bool { return k == Refund || k == Rollback }

// MovesBalance indica se o tipo altera o saldo quando processado.
//
// `LOSS` é o único tipo externo sem movimentação: não gera lançamento no ledger
// nem incrementa a versão da carteira.
func (k Kind) MovesBalance() bool { return k != Loss }

// ValidateAmount aplica a política de valor de cada tipo.
//
// `LOSS` exige exatamente zero; os demais exigem valor maior que zero. Valores
// negativos nunca são aceitos na entrada externa.
func (k Kind) ValidateAmount(amount money.Money) error {
	if err := amount.Validate(); err != nil {
		return err
	}

	if k == Loss {
		if !amount.IsZero() {
			return ErrInvalidAmountForKind.Messagef("LOSS exige valor 0.00, recebido %s", amount)
		}
		return nil
	}

	if !amount.IsPositive() {
		return ErrInvalidAmountForKind.Messagef("%s exige valor maior que zero, recebido %s", k, amount)
	}
	return nil
}

// ValidateReference aplica a política de referência de cada tipo.
//
// `REFUND` e `ROLLBACK` exigem referência. `WIN` pode informar a aposta da mesma
// rodada. Os demais não aceitam referência.
func (k Kind) ValidateReference(reference string) error {
	switch {
	case k.IsReversal() && reference == "":
		return ErrReferenceRequired.Messagef("%s exige referenceExternalTransactionId", k)
	case !k.IsReversal() && k != Win && reference != "":
		return ErrReferenceNotAllowed.Messagef("%s não aceita referência, recebido %q", k, reference)
	default:
		return nil
	}
}
