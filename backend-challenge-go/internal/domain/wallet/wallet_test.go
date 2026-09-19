package wallet_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

var (
	walletID = mustID("0192f291-27dd-7d3f-8071-5f8685deef37")
	playerID = mustID("0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1")
	now      = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	later    = now.Add(time.Minute)
)

func mustID(raw string) shared.ID {
	parsed, err := shared.ParseID(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

func brl(t *testing.T, amount string) money.Money {
	t.Helper()
	parsed, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("Parse(%q) devolveu erro: %v", amount, err)
	}
	return parsed
}

func openWallet(t *testing.T, initial string) *wallet.Wallet {
	t.Helper()
	opened, err := wallet.Open(walletID, playerID, brl(t, initial), now)
	if err != nil {
		t.Fatalf("Open devolveu erro: %v", err)
	}
	return opened
}

func TestOpenWithPositiveBalance(t *testing.T) {
	opened := openWallet(t, "1000.00")

	if !opened.ID().Equal(walletID) || !opened.PlayerID().Equal(playerID) {
		t.Errorf("identidade incorreta: %s / %s", opened.ID(), opened.PlayerID())
	}
	if opened.Currency() != money.BRL {
		t.Errorf("Currency = %q, esperado BRL", opened.Currency())
	}
	if opened.Balance().String() != "1000.00" {
		t.Errorf("Balance = %s, esperado 1000.00", opened.Balance())
	}
	// A abertura com crédito inicial mantém a versão em 1: a versão só passa a
	// contar movimentações depois da criação.
	if opened.Version() != 1 {
		t.Errorf("Version = %d, esperado 1", opened.Version())
	}
	if !opened.CreatedAt().Equal(now) || !opened.UpdatedAt().Equal(now) {
		t.Errorf("instantes = %s / %s", opened.CreatedAt(), opened.UpdatedAt())
	}
}

func TestOpenWithZeroBalance(t *testing.T) {
	opened := openWallet(t, "0.00")

	if !opened.Balance().IsZero() {
		t.Errorf("Balance = %s, esperado 0.00", opened.Balance())
	}
	if opened.Version() != 1 {
		t.Errorf("Version = %d, esperado 1", opened.Version())
	}
}

func TestOpenNormalizesTimestampToUTC(t *testing.T) {
	saoPaulo := time.FixedZone("America/Sao_Paulo", -3*60*60)
	local := time.Date(2026, 9, 8, 9, 0, 0, 0, saoPaulo)

	opened, err := wallet.Open(walletID, playerID, brl(t, "10.00"), local)
	if err != nil {
		t.Fatalf("Open devolveu erro: %v", err)
	}

	if opened.CreatedAt().Location() != time.UTC {
		t.Errorf("Location = %s, esperado UTC", opened.CreatedAt().Location())
	}
	if !opened.CreatedAt().Equal(local) {
		t.Errorf("o instante deveria ser preservado: %s != %s", opened.CreatedAt(), local)
	}
}

func TestOpenRejectsInvalidInput(t *testing.T) {
	var zeroID shared.ID
	var uninitializedMoney money.Money

	tests := []struct {
		name     string
		id       shared.ID
		player   shared.ID
		balance  money.Money
		moment   time.Time
		wantCode error
	}{
		{"carteira sem id", zeroID, playerID, brl(t, "10.00"), now, shared.ErrInvalidID},
		{"jogador sem id", walletID, zeroID, brl(t, "10.00"), now, shared.ErrInvalidID},
		{"saldo não inicializado", walletID, playerID, uninitializedMoney, now, money.ErrUninitialized},
		{"saldo negativo", walletID, playerID, money.FromMinorUnits(-1, money.BRL), now, wallet.ErrNegativeInitialBalance},
		{"sem instante", walletID, playerID, brl(t, "10.00"), time.Time{}, wallet.ErrInvalidState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := wallet.Open(tt.id, tt.player, tt.balance, tt.moment)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}

func TestCreditIncreasesBalanceAndVersion(t *testing.T) {
	opened := openWallet(t, "1000.00")

	movement, err := opened.Credit(brl(t, "25.00"), later)
	if err != nil {
		t.Fatalf("Credit devolveu erro: %v", err)
	}

	if opened.Balance().String() != "1025.00" {
		t.Errorf("Balance = %s, esperado 1025.00", opened.Balance())
	}
	if opened.Version() != 2 {
		t.Errorf("Version = %d, esperado 2", opened.Version())
	}
	if !opened.UpdatedAt().Equal(later) {
		t.Errorf("UpdatedAt = %s, esperado %s", opened.UpdatedAt(), later)
	}
	if movement.BalanceBefore.String() != "1000.00" || movement.BalanceAfter.String() != "1025.00" {
		t.Errorf("movimento = %+v", movement)
	}
	if movement.Amount.String() != "25.00" || movement.Version != 2 {
		t.Errorf("movimento = %+v", movement)
	}
}

func TestDebitDecreasesBalanceAndVersion(t *testing.T) {
	opened := openWallet(t, "1000.00")

	movement, err := opened.Debit(brl(t, "25.00"), later)
	if err != nil {
		t.Fatalf("Debit devolveu erro: %v", err)
	}

	if opened.Balance().String() != "975.00" {
		t.Errorf("Balance = %s, esperado 975.00", opened.Balance())
	}
	if opened.Version() != 2 {
		t.Errorf("Version = %d, esperado 2", opened.Version())
	}
	if movement.BalanceBefore.String() != "1000.00" || movement.BalanceAfter.String() != "975.00" {
		t.Errorf("movimento = %+v", movement)
	}
}

func TestDebitDownToZeroIsAllowed(t *testing.T) {
	opened := openWallet(t, "100.00")

	if _, err := opened.Debit(brl(t, "100.00"), later); err != nil {
		t.Fatalf("Debit devolveu erro: %v", err)
	}
	if !opened.Balance().IsZero() {
		t.Errorf("Balance = %s, esperado 0.00", opened.Balance())
	}
}

// Cenário obrigatório do desafio: a segunda aposta de 80.00 sobre um saldo de
// 100.00 precisa ser recusada sem alterar saldo nem versão.
func TestDebitBeyondBalanceIsRejectedWithoutSideEffects(t *testing.T) {
	opened := openWallet(t, "100.00")
	if _, err := opened.Debit(brl(t, "80.00"), later); err != nil {
		t.Fatalf("primeiro débito devolveu erro: %v", err)
	}

	_, err := opened.Debit(brl(t, "80.00"), later.Add(time.Second))

	if !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("erro = %v, esperado ErrInsufficientFunds", err)
	}
	if opened.Balance().String() != "20.00" {
		t.Errorf("Balance = %s, esperado 20.00", opened.Balance())
	}
	if opened.Version() != 2 {
		t.Errorf("Version = %d, esperado 2 (a recusa não incrementa versão)", opened.Version())
	}
	if !opened.UpdatedAt().Equal(later) {
		t.Errorf("UpdatedAt = %s, a recusa não deveria alterar o instante", opened.UpdatedAt())
	}
}

func TestMovementsRejectInvalidInput(t *testing.T) {
	var uninitialized money.Money
	usd, err := money.Parse("10.00", "USD")
	if err != nil {
		t.Fatalf("Parse devolveu erro: %v", err)
	}

	tests := []struct {
		name     string
		amount   money.Money
		moment   time.Time
		wantCode error
	}{
		{"valor não inicializado", uninitialized, later, money.ErrUninitialized},
		{"moeda incompatível", usd, later, money.ErrCurrencyMismatch},
		{"valor zero", brl(t, "0.00"), later, wallet.ErrNonPositiveMovement},
		{"valor negativo", money.FromMinorUnits(-1, money.BRL), later, wallet.ErrNonPositiveMovement},
		{"sem instante", brl(t, "10.00"), time.Time{}, wallet.ErrInvalidState},
	}

	for _, tt := range tests {
		for _, operation := range []struct {
			name string
			call func(*wallet.Wallet) (wallet.Movement, error)
		}{
			{"Credit", func(w *wallet.Wallet) (wallet.Movement, error) { return w.Credit(tt.amount, tt.moment) }},
			{"Debit", func(w *wallet.Wallet) (wallet.Movement, error) { return w.Debit(tt.amount, tt.moment) }},
		} {
			t.Run(operation.name+"/"+tt.name, func(t *testing.T) {
				opened := openWallet(t, "1000.00")

				if _, err := operation.call(opened); !errors.Is(err, tt.wantCode) {
					t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
				}
				if opened.Version() != 1 || opened.Balance().String() != "1000.00" {
					t.Errorf("a recusa não deveria alterar o agregado: versão %d, saldo %s",
						opened.Version(), opened.Balance())
				}
			})
		}
	}
}

func TestMovementsDetectOverflow(t *testing.T) {
	nearMaximum, err := wallet.Rehydrate(wallet.State{
		ID:        walletID,
		PlayerID:  playerID,
		Balance:   money.FromMinorUnits(math.MaxInt64, money.BRL),
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("Rehydrate devolveu erro: %v", err)
	}

	if _, err := nearMaximum.Credit(brl(t, "0.01"), later); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Credit: erro = %v, esperado ErrOverflow", err)
	}
	if nearMaximum.Version() != 1 {
		t.Errorf("Version = %d, o estouro não deveria alterar o agregado", nearMaximum.Version())
	}
}

func TestRehydrateRestoresStateWithoutReapplyingMovements(t *testing.T) {
	restored, err := wallet.Rehydrate(wallet.State{
		ID:        walletID,
		PlayerID:  playerID,
		Balance:   brl(t, "975.00"),
		Version:   7,
		CreatedAt: now,
		UpdatedAt: later,
	})
	if err != nil {
		t.Fatalf("Rehydrate devolveu erro: %v", err)
	}

	if restored.Balance().String() != "975.00" {
		t.Errorf("Balance = %s, esperado 975.00", restored.Balance())
	}
	if restored.Version() != 7 {
		t.Errorf("Version = %d, esperado 7 (reidratação não incrementa)", restored.Version())
	}
	if restored.Currency() != money.BRL {
		t.Errorf("Currency = %q, esperado BRL", restored.Currency())
	}
	if !restored.CreatedAt().Equal(now) || !restored.UpdatedAt().Equal(later) {
		t.Errorf("instantes = %s / %s", restored.CreatedAt(), restored.UpdatedAt())
	}
}

func TestRehydrateRejectsInconsistentState(t *testing.T) {
	var zeroID shared.ID
	var uninitializedMoney money.Money

	valid := wallet.State{
		ID:        walletID,
		PlayerID:  playerID,
		Balance:   brl(t, "975.00"),
		Version:   7,
		CreatedAt: now,
		UpdatedAt: later,
	}

	tests := []struct {
		name     string
		mutate   func(*wallet.State)
		wantCode error
	}{
		{"sem id", func(s *wallet.State) { s.ID = zeroID }, shared.ErrInvalidID},
		{"sem jogador", func(s *wallet.State) { s.PlayerID = zeroID }, shared.ErrInvalidID},
		{"saldo não inicializado", func(s *wallet.State) { s.Balance = uninitializedMoney }, money.ErrUninitialized},
		{"saldo negativo", func(s *wallet.State) { s.Balance = money.FromMinorUnits(-1, money.BRL) }, wallet.ErrInvalidState},
		{"versão zero", func(s *wallet.State) { s.Version = 0 }, wallet.ErrInvalidState},
		{"versão negativa", func(s *wallet.State) { s.Version = -1 }, wallet.ErrInvalidState},
		{"sem criação", func(s *wallet.State) { s.CreatedAt = time.Time{} }, wallet.ErrInvalidState},
		{"sem atualização", func(s *wallet.State) { s.UpdatedAt = time.Time{} }, wallet.ErrInvalidState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := valid
			tt.mutate(&state)

			_, err := wallet.Rehydrate(state)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}
