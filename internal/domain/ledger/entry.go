// Package ledger implementa o lançamento contábil da carteira.
//
// O ledger é append-only: correções financeiras exigem lançamentos novos, nunca
// edição ou exclusão. A imutabilidade é garantida aqui pelo encapsulamento e no
// banco por constraints e revogação de UPDATE/DELETE.
package ledger

import (
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// Erros do pacote. A comparação é por código, via `errors.Is`.
var (
	// ErrInvalidDirection cobre direções fora de DEBIT/CREDIT.
	ErrInvalidDirection = shared.Validation("INVALID_LEDGER_DIRECTION", "direção de lançamento inválida")
	// ErrInconsistentEntry cobre lançamentos que não fecham a equação de saldo.
	ErrInconsistentEntry = shared.Invariant("INCONSISTENT_LEDGER_ENTRY", "lançamento inconsistente")
)

// Direction é o sentido do lançamento.
type Direction string

// Direções possíveis.
const (
	Debit  Direction = "DEBIT"
	Credit Direction = "CREDIT"
)

// ParseDirection converte a representação textual persistida.
func ParseDirection(raw string) (Direction, error) {
	switch Direction(raw) {
	case Debit:
		return Debit, nil
	case Credit:
		return Credit, nil
	default:
		return "", ErrInvalidDirection.Messagef("direção %q não é DEBIT nem CREDIT", raw)
	}
}

// String devolve a representação textual.
func (d Direction) String() string { return string(d) }

// Entry é um lançamento imutável na carteira.
//
// Os campos são privados e só há leitura: depois de construído, um lançamento
// não muda.
type Entry struct {
	id            shared.ID
	walletID      shared.ID
	transactionID shared.ID
	direction     Direction
	amount        money.Money
	balanceBefore money.Money
	balanceAfter  money.Money
	createdAt     time.Time
}

// NewEntry cria um lançamento, validando a equação de saldo.
//
// A validação é a mesma em ambos os sentidos: `balanceAfter` precisa ser
// exatamente `balanceBefore` mais ou menos o valor, conforme a direção.
func NewEntry(
	id, walletID, transactionID shared.ID,
	direction Direction,
	amount, balanceBefore, balanceAfter money.Money,
	createdAt time.Time,
) (Entry, error) {
	for _, identifier := range []shared.ID{id, walletID, transactionID} {
		if err := identifier.Validate(); err != nil {
			return Entry{}, err
		}
	}
	if direction != Debit && direction != Credit {
		return Entry{}, ErrInvalidDirection.Messagef("direção %q não é DEBIT nem CREDIT", direction)
	}
	for _, value := range []money.Money{amount, balanceBefore, balanceAfter} {
		if err := value.Validate(); err != nil {
			return Entry{}, err
		}
	}
	if !amount.IsPositive() {
		return Entry{}, ErrInconsistentEntry.Messagef("lançamento com valor não positivo: %s", amount)
	}
	if err := requireBalanceEquation(direction, amount, balanceBefore, balanceAfter); err != nil {
		return Entry{}, err
	}
	if balanceAfter.IsNegative() {
		return Entry{}, ErrInconsistentEntry.Messagef("saldo posterior negativo: %s", balanceAfter)
	}
	if createdAt.IsZero() {
		return Entry{}, ErrInconsistentEntry.Messagef("instante de criação não informado")
	}

	return Entry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		amount:        amount,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     createdAt.UTC(),
	}, nil
}

func requireBalanceEquation(direction Direction, amount, before, after money.Money) error {
	var expected money.Money
	var err error

	if direction == Credit {
		expected, err = before.Add(amount)
	} else {
		expected, err = before.Sub(amount)
	}
	if err != nil {
		return err
	}
	if !expected.Equal(after) {
		return ErrInconsistentEntry.Messagef(
			"saldo posterior %s não corresponde a %s %s %s", after, before, sign(direction), amount)
	}
	return nil
}

func sign(direction Direction) string {
	if direction == Credit {
		return "+"
	}
	return "-"
}

// ID devolve o identificador do lançamento.
func (e Entry) ID() shared.ID { return e.id }

// WalletID devolve a carteira movimentada.
func (e Entry) WalletID() shared.ID { return e.walletID }

// TransactionID devolve a transação que originou o lançamento.
func (e Entry) TransactionID() shared.ID { return e.transactionID }

// Direction devolve o sentido do lançamento.
func (e Entry) Direction() Direction { return e.direction }

// Amount devolve o valor movimentado.
func (e Entry) Amount() money.Money { return e.amount }

// BalanceBefore devolve o saldo anterior ao lançamento.
func (e Entry) BalanceBefore() money.Money { return e.balanceBefore }

// BalanceAfter devolve o saldo posterior ao lançamento.
func (e Entry) BalanceAfter() money.Money { return e.balanceAfter }

// CreatedAt devolve o instante de criação, em UTC.
func (e Entry) CreatedAt() time.Time { return e.createdAt }
