package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

const playerID = "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"

var fixedNow = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

type fixture struct {
	unitOfWork *fakeUnitOfWork
	clock      *fakeClock
	ids        *fakeIDs
	open       *usecase.OpenWallet
	process    *usecase.ProcessWagerTransaction
	reconcile  *usecase.ReconcileWallet
}

func newFixture() *fixture {
	unitOfWork := newFakeUnitOfWork()
	clock := &fakeClock{now: fixedNow}
	ids := &fakeIDs{}

	return &fixture{
		unitOfWork: unitOfWork,
		clock:      clock,
		ids:        ids,
		open:       usecase.NewOpenWallet(unitOfWork, clock, ids),
		process:    usecase.NewProcessWagerTransaction(unitOfWork, clock, ids),
		reconcile:  usecase.NewReconcileWallet(unitOfWork),
	}
}

func openWalletCommand(amount string) usecase.OpenWalletCommand {
	return usecase.OpenWalletCommand{
		PlayerID:       playerID,
		InitialBalance: usecase.MoneyInput{Amount: amount, Currency: "BRL"},
	}
}

func TestOpenWalletWithPositiveBalance(t *testing.T) {
	f := newFixture()

	result, err := f.open.Execute(context.Background(), openWalletCommand("1000.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	opened := result.Wallet
	if opened.Balance().String() != "1000.00" {
		t.Errorf("saldo = %s, esperado 1000.00", opened.Balance())
	}
	// A abertura com crédito mantém a versão em 1.
	if opened.Version() != 1 {
		t.Errorf("versão = %d, esperada 1", opened.Version())
	}

	// A abertura cria OPENING processada e o lançamento de crédito no mesmo
	// commit da carteira.
	if total := len(f.unitOfWork.state.transactions); total != 1 {
		t.Fatalf("transações = %d, esperada 1", total)
	}
	for _, transaction := range f.unitOfWork.state.transactions {
		if transaction.Kind() != wagering.Opening || transaction.Status() != wagering.Processed {
			t.Errorf("transação = %s / %s", transaction.Kind(), transaction.Status())
		}
		if transaction.Origin() != wagering.OriginInternal || transaction.ProviderID() != "" {
			t.Error("a abertura não deveria carregar metadados externos")
		}
	}
	if entries := len(f.unitOfWork.state.ledger); entries != 1 {
		t.Fatalf("lançamentos = %d, esperado 1", entries)
	}
	if entry := f.unitOfWork.state.ledger[0]; entry.BalanceBefore().String() != "0.00" ||
		entry.BalanceAfter().String() != "1000.00" {
		t.Errorf("lançamento = %s → %s", entry.BalanceBefore(), entry.BalanceAfter())
	}
}

// Saldo inicial zero não cria OPENING, ledger nem eventos financeiros.
func TestOpenWalletWithZeroBalance(t *testing.T) {
	f := newFixture()

	result, err := f.open.Execute(context.Background(), openWalletCommand("0.00"))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if !result.Wallet.Balance().IsZero() || result.Wallet.Version() != 1 {
		t.Errorf("carteira = %s / versão %d", result.Wallet.Balance(), result.Wallet.Version())
	}
	if len(f.unitOfWork.state.transactions) != 0 || len(f.unitOfWork.state.ledger) != 0 {
		t.Error("saldo zero não deveria criar transação nem lançamento")
	}
}

func TestOpenWalletRejectsDuplicate(t *testing.T) {
	f := newFixture()
	if _, err := f.open.Execute(context.Background(), openWalletCommand("100.00")); err != nil {
		t.Fatalf("primeira abertura devolveu erro: %v", err)
	}

	_, err := f.open.Execute(context.Background(), openWalletCommand("100.00"))

	if !errors.Is(err, usecase.ErrWalletAlreadyExists) {
		t.Errorf("erro = %v, esperado ErrWalletAlreadyExists", err)
	}
}

func TestOpenWalletRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		command  usecase.OpenWalletCommand
		wantCode error
	}{
		{
			name:     "jogador inválido",
			command:  usecase.OpenWalletCommand{PlayerID: "jogador-1", InitialBalance: usecase.MoneyInput{Amount: "10.00", Currency: "BRL"}},
			wantCode: usecase.ErrInvalidInput,
		},
		{
			name:     "valor não canônico",
			command:  openWalletCommand("10.0"),
			wantCode: money.ErrInvalidAmount,
		},
		{
			name:     "moeda inválida",
			command:  usecase.OpenWalletCommand{PlayerID: playerID, InitialBalance: usecase.MoneyInput{Amount: "10.00", Currency: "brl"}},
			wantCode: money.ErrInvalidCurrency,
		},
		{
			name:     "saldo negativo",
			command:  openWalletCommand("-10.00"),
			wantCode: usecase.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()

			_, err := f.open.Execute(context.Background(), tt.command)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}

// Uma falha no meio da abertura desfaz tudo: não pode sobrar carteira sem o
// crédito inicial correspondente.
func TestOpenWalletRollsBackOnFailure(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fixture)
	}{
		{
			name:  "falha ao gerar id da transação",
			setup: func(f *fixture) { f.ids.failAfter = 2 },
		},
		{
			name:  "falha ao gravar a transação",
			setup: func(f *fixture) { f.unitOfWork.state.transactionErr = errors.New("falha simulada") },
		},
		{
			name:  "falha ao gravar o lançamento",
			setup: func(f *fixture) { f.unitOfWork.state.ledgerAppendErr = errors.New("falha simulada") },
		},
		{
			name:  "falha ao gerar id do lançamento",
			setup: func(f *fixture) { f.ids.failAfter = 3 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			tt.setup(f)

			_, err := f.open.Execute(context.Background(), openWalletCommand("100.00"))

			if err == nil {
				t.Fatal("esperava erro")
			}
			if len(f.unitOfWork.state.wallets) != 0 {
				t.Error("a carteira não deveria ter sido persistida")
			}
			if len(f.unitOfWork.state.ledger) != 0 {
				t.Error("o lançamento não deveria ter sido persistido")
			}
		})
	}
}

func TestOpenWalletPropagatesInfrastructureFailures(t *testing.T) {
	t.Run("falha ao gerar o id da carteira", func(t *testing.T) {
		f := newFixture()
		f.ids.failAfter = 1

		if _, err := f.open.Execute(context.Background(), openWalletCommand("100.00")); err == nil {
			t.Error("esperava erro")
		}
	})

	t.Run("falha ao abrir a transação", func(t *testing.T) {
		f := newFixture()
		f.unitOfWork.beginErr = errors.New("banco indisponível")

		if _, err := f.open.Execute(context.Background(), openWalletCommand("100.00")); err == nil {
			t.Error("esperava erro")
		}
	})

	t.Run("falha ao criar a carteira", func(t *testing.T) {
		f := newFixture()
		f.unitOfWork.state.walletCreateErr = errors.New("falha simulada")

		if _, err := f.open.Execute(context.Background(), openWalletCommand("100.00")); err == nil {
			t.Error("esperava erro")
		}
	})

	// O relógio sem instante torna a carteira inválida: o domínio recusa antes
	// de qualquer escrita.
	t.Run("relógio sem instante", func(t *testing.T) {
		f := newFixture()
		f.clock.now = time.Time{}

		if _, err := f.open.Execute(context.Background(), openWalletCommand("100.00")); err == nil {
			t.Error("esperava erro")
		}
	})
}
