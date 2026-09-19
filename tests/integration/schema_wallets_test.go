//go:build integration

package integration

import (
	"testing"
)

func TestWalletConstraints(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	t.Run("saldo negativo é recusado pelo banco", func(t *testing.T) {
		row := newWalletRow(t)
		row.Balance = -1

		if err := insertWallet(ctx, row); err == nil {
			t.Fatal("esperava recusa do CHECK de saldo não negativo")
		} else {
			assertConstraint(t, err, "wallets_balance_non_negative")
		}
	})

	t.Run("versão menor que um é recusada", func(t *testing.T) {
		row := newWalletRow(t)
		row.Version = 0

		if err := insertWallet(ctx, row); err == nil {
			t.Fatal("esperava recusa do CHECK de versão")
		} else {
			assertConstraint(t, err, "wallets_version_positive")
		}
	})

	t.Run("moeda fora do formato ISO 4217 é recusada", func(t *testing.T) {
		for _, currency := range []string{"brl", "BR ", "12A"} {
			row := newWalletRow(t)
			row.Currency = currency

			if err := insertWallet(ctx, row); err == nil {
				t.Errorf("esperava recusa para a moeda %q", currency)
			} else {
				assertConstraint(t, err, "wallets_currency_is_iso4217")
			}
		}
	})

	// A unicidade de (playerId, currency) é o que impede duas carteiras
	// concorrentes para o mesmo jogador na mesma moeda.
	t.Run("jogador tem uma única carteira por moeda", func(t *testing.T) {
		first := mustInsertWallet(t, ctx)

		second := newWalletRow(t)
		second.PlayerID = first.PlayerID
		second.Currency = first.Currency

		if err := insertWallet(ctx, second); err == nil {
			t.Fatal("esperava conflito de unicidade")
		} else {
			assertConstraint(t, err, "wallets_player_currency_unique")
		}
	})

	t.Run("mesmo jogador pode ter carteiras em moedas distintas", func(t *testing.T) {
		first := mustInsertWallet(t, ctx)

		second := newWalletRow(t)
		second.PlayerID = first.PlayerID
		second.Currency = "USD"

		if err := insertWallet(ctx, second); err != nil {
			t.Fatalf("moedas distintas deveriam ser aceitas: %v", err)
		}
	})
}
