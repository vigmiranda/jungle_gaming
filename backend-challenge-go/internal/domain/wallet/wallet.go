// Package wallet implementa a raiz do agregado financeiro.
//
// O saldo só muda por métodos do agregado, que preservam as invariantes:
// saldo maior ou igual a zero, moeda compatível e versão incrementada apenas
// quando houve movimentação.
package wallet

import (
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// Erros do agregado. A comparação é por código, via `errors.Is`.
var (
	// ErrInsufficientFunds cobre o débito que deixaria o saldo negativo.
	ErrInsufficientFunds = shared.Rejection("INSUFFICIENT_FUNDS", "saldo insuficiente")
	// ErrNonPositiveMovement cobre movimentações de valor zero ou negativo.
	ErrNonPositiveMovement = shared.Validation("NON_POSITIVE_MOVEMENT", "movimentação deve ter valor positivo")
	// ErrNegativeInitialBalance cobre a abertura com saldo negativo.
	ErrNegativeInitialBalance = shared.Validation("NEGATIVE_INITIAL_BALANCE", "saldo inicial não pode ser negativo")
	// ErrInvalidState cobre reidratação a partir de dados inconsistentes.
	ErrInvalidState = shared.Invariant("INVALID_WALLET_STATE", "estado de carteira inválido")
)

// initialVersion é a versão atribuída na abertura, inclusive quando a abertura
// credita o saldo inicial.
const initialVersion int64 = 1

// Wallet é a raiz do agregado financeiro de um jogador em uma moeda.
type Wallet struct {
	id        shared.ID
	playerID  shared.ID
	currency  money.Currency
	balance   money.Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

// Open cria uma carteira nova.
//
// O saldo inicial já nasce aplicado e a versão é 1, mesmo quando há crédito de
// abertura: a versão só passa a contar movimentações depois da criação.
func Open(id, playerID shared.ID, initialBalance money.Money, now time.Time) (*Wallet, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if err := playerID.Validate(); err != nil {
		return nil, err
	}
	if err := initialBalance.Validate(); err != nil {
		return nil, err
	}
	if initialBalance.IsNegative() {
		return nil, ErrNegativeInitialBalance.Messagef("saldo inicial %s", initialBalance)
	}
	if now.IsZero() {
		return nil, ErrInvalidState.Messagef("instante de criação não informado")
	}

	return &Wallet{
		id:        id,
		playerID:  playerID,
		currency:  initialBalance.Currency(),
		balance:   initialBalance,
		version:   initialVersion,
		createdAt: now.UTC(),
		updatedAt: now.UTC(),
	}, nil
}

// State é a fotografia persistida da carteira, usada na reidratação.
type State struct {
	ID        shared.ID
	PlayerID  shared.ID
	Balance   money.Money
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Rehydrate reconstrói a carteira a partir do estado persistido.
//
// A reidratação não reaplica movimentações nem emite eventos: apenas valida a
// consistência do que veio do banco.
func Rehydrate(state State) (*Wallet, error) {
	if err := state.ID.Validate(); err != nil {
		return nil, err
	}
	if err := state.PlayerID.Validate(); err != nil {
		return nil, err
	}
	if err := state.Balance.Validate(); err != nil {
		return nil, err
	}
	if state.Balance.IsNegative() {
		return nil, ErrInvalidState.Messagef("saldo persistido negativo: %s", state.Balance)
	}
	if state.Version < initialVersion {
		return nil, ErrInvalidState.Messagef("versão persistida inválida: %d", state.Version)
	}
	if state.CreatedAt.IsZero() || state.UpdatedAt.IsZero() {
		return nil, ErrInvalidState.Messagef("instantes de criação e atualização são obrigatórios")
	}

	return &Wallet{
		id:        state.ID,
		playerID:  state.PlayerID,
		currency:  state.Balance.Currency(),
		balance:   state.Balance,
		version:   state.Version,
		createdAt: state.CreatedAt.UTC(),
		updatedAt: state.UpdatedAt.UTC(),
	}, nil
}

// ID devolve o identificador da carteira.
func (w *Wallet) ID() shared.ID { return w.id }

// PlayerID devolve o jogador dono da carteira.
func (w *Wallet) PlayerID() shared.ID { return w.playerID }

// Currency devolve a moeda da carteira.
func (w *Wallet) Currency() money.Currency { return w.currency }

// Balance devolve o saldo atual.
func (w *Wallet) Balance() money.Money { return w.balance }

// Version devolve a versão atual.
func (w *Wallet) Version() int64 { return w.version }

// CreatedAt devolve o instante de criação, em UTC.
func (w *Wallet) CreatedAt() time.Time { return w.createdAt }

// UpdatedAt devolve o instante da última alteração, em UTC.
func (w *Wallet) UpdatedAt() time.Time { return w.updatedAt }

// Movement descreve o efeito de uma movimentação no saldo.
//
// Os três valores alimentam o lançamento no ledger e o evento
// `WalletBalanceChanged`, evitando que a borda recalcule o que o agregado já
// sabe.
type Movement struct {
	Amount        money.Money
	BalanceBefore money.Money
	BalanceAfter  money.Money
	Version       int64
}

// Credit soma o valor ao saldo.
func (w *Wallet) Credit(amount money.Money, now time.Time) (Movement, error) {
	if err := w.validateMovement(amount, now); err != nil {
		return Movement{}, err
	}

	before := w.balance
	after, err := before.Add(amount)
	if err != nil {
		return Movement{}, err
	}
	return w.apply(amount, before, after, now), nil
}

// Debit subtrai o valor do saldo, preservando saldo maior ou igual a zero.
func (w *Wallet) Debit(amount money.Money, now time.Time) (Movement, error) {
	if err := w.validateMovement(amount, now); err != nil {
		return Movement{}, err
	}

	before := w.balance
	after, err := before.Sub(amount)
	if err != nil {
		return Movement{}, err
	}
	if after.IsNegative() {
		return Movement{}, ErrInsufficientFunds.Messagef(
			"saldo %s é insuficiente para debitar %s", before, amount)
	}
	return w.apply(amount, before, after, now), nil
}

func (w *Wallet) validateMovement(amount money.Money, now time.Time) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	if amount.Currency() != w.currency {
		return money.ErrCurrencyMismatch.Messagef(
			"movimentação em %s numa carteira em %s", amount.Currency(), w.currency)
	}
	if !amount.IsPositive() {
		return ErrNonPositiveMovement.Messagef("valor informado: %s", amount)
	}
	if now.IsZero() {
		return ErrInvalidState.Messagef("instante da movimentação não informado")
	}
	return nil
}

func (w *Wallet) apply(amount, before, after money.Money, now time.Time) Movement {
	w.balance = after
	w.version++
	w.updatedAt = now.UTC()

	return Movement{
		Amount:        amount,
		BalanceBefore: before,
		BalanceAfter:  after,
		Version:       w.version,
	}
}
