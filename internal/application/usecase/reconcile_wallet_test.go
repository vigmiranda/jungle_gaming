package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

func parseID(t *testing.T, raw string) shared.ID {
	t.Helper()
	parsed, err := shared.ParseID(raw)
	if err != nil {
		t.Fatalf("ParseID devolveu erro: %v", err)
	}
	return parsed
}

func TestReconcileReportsConsistentWallet(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")
	if _, err := f.process.Execute(context.Background(), betCommand(walletID, "bet-1", "25.00")); err != nil {
		t.Fatalf("aposta devolveu erro: %v", err)
	}

	result, err := f.reconcile.Execute(context.Background(), parseID(t, walletID))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if !result.Consistent {
		t.Errorf("esperava carteira consistente: %+v", result)
	}
	if result.StoredBalance.String() != "975.00" || result.CalculatedBalance.String() != "975.00" {
		t.Errorf("saldos = %s / %s", result.StoredBalance, result.CalculatedBalance)
	}
	if result.Difference.String() != "0.00" {
		t.Errorf("diferença = %s, esperada 0.00", result.Difference)
	}
	// O crédito de abertura conta como lançamento.
	if result.CheckedEntries != 2 {
		t.Errorf("lançamentos = %d, esperados 2", result.CheckedEntries)
	}
}

func TestReconcileWalletWithoutLedgerEntries(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "0.00")

	result, err := f.reconcile.Execute(context.Background(), parseID(t, walletID))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if !result.Consistent || result.CheckedEntries != 0 {
		t.Errorf("resultado = %+v", result)
	}
	// Sem lançamentos, o comparativo usa o zero da moeda da carteira.
	if result.CalculatedBalance.String() != "0.00" || result.CalculatedBalance.Currency() != money.BRL {
		t.Errorf("saldo reconstruído = %s %s", result.CalculatedBalance, result.CalculatedBalance.Currency())
	}
}

// Divergência é reportada, nunca corrigida em silêncio.
func TestReconcileReportsDivergenceWithoutChangingBalance(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	// Simula um ledger que não fecha com o saldo armazenado.
	divergent := money.FromMinorUnits(90000, money.BRL)
	f.unitOfWork.state.ledgerSumMoney = &divergent

	result, err := f.reconcile.Execute(context.Background(), parseID(t, walletID))
	if err != nil {
		t.Fatalf("Execute devolveu erro: %v", err)
	}

	if result.Consistent {
		t.Error("esperava divergência")
	}
	if result.Difference.String() != "100.00" {
		t.Errorf("diferença = %s, esperada 100.00 (armazenado menos reconstruído)", result.Difference)
	}
	if balance := f.unitOfWork.state.wallets[walletID].wallet.Balance().String(); balance != "1000.00" {
		t.Errorf("saldo = %s, a reconciliação não deveria alterá-lo", balance)
	}
}

func TestReconcilePropagatesFailures(t *testing.T) {
	failure := errors.New("falha simulada")

	tests := []struct {
		name  string
		setup func(*fixture)
	}{
		{"leitura da carteira", func(f *fixture) { f.unitOfWork.state.walletFindErr = failure }},
		{"soma do ledger", func(f *fixture) { f.unitOfWork.state.ledgerSumErr = failure }},
		{"início da transação", func(f *fixture) { f.unitOfWork.beginErr = failure }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			walletID := setupWallet(t, f, "100.00")
			tt.setup(f)

			if _, err := f.reconcile.Execute(context.Background(), parseID(t, walletID)); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

// Moedas incompatíveis entre saldo e ledger indicam dados corrompidos: a
// comparação precisa falhar em vez de inventar um resultado.
func TestReconcileFailsOnCurrencyMismatch(t *testing.T) {
	f := newFixture()
	walletID := setupWallet(t, f, "1000.00")

	foreign := money.FromMinorUnits(100000, "USD")
	f.unitOfWork.state.ledgerSumMoney = &foreign

	_, err := f.reconcile.Execute(context.Background(), parseID(t, walletID))

	if !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("erro = %v, esperado ErrCurrencyMismatch", err)
	}
}

func TestReconcileRejectsUnknownWallet(t *testing.T) {
	f := newFixture()

	_, err := f.reconcile.Execute(context.Background(),
		parseID(t, "0192f291-27dd-7d3f-8071-5f8685deef37"))

	if err == nil {
		t.Error("esperava erro para carteira inexistente")
	}
}
