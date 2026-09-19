//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"
)

// A idempotência precisa sobreviver ao reinício de todos os processos, então
// ela vive no schema e não na memória da aplicação.
func TestTransactionIdempotencyConstraints(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)

	t.Run("mesmo par provedor e id externo é recusado", func(t *testing.T) {
		first := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		duplicate := newExternalTransaction(t, wallet)
		duplicate.ExternalID = first.ExternalID

		if err := insertTransaction(ctx, duplicate); err == nil {
			t.Fatal("esperava conflito de unicidade")
		} else {
			assertConstraint(t, err, "wager_transactions_provider_external_id_uq")
		}
	})

	t.Run("mesma chave de idempotência é recusada", func(t *testing.T) {
		first := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		duplicate := newExternalTransaction(t, wallet)
		duplicate.IdempotencyKey = first.IdempotencyKey

		if err := insertTransaction(ctx, duplicate); err == nil {
			t.Fatal("esperava conflito de unicidade")
		} else {
			assertConstraint(t, err, "wager_transactions_provider_idempotency_key_uq")
		}
	})

	// O escopo é por provedor: a chave pertence ao cliente e não deve colidir
	// entre tenants diferentes.
	t.Run("provedores distintos podem repetir chave e id externo", func(t *testing.T) {
		first := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		other := newExternalTransaction(t, wallet)
		other.ProviderID = text("provider-b")
		other.ExternalID = first.ExternalID
		other.IdempotencyKey = first.IdempotencyKey

		if err := insertTransaction(ctx, other); err != nil {
			t.Fatalf("provedores distintos deveriam conviver: %v", err)
		}
	})
}

func TestTransactionAmountPolicyPerKind(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)

	t.Run("LOSS exige valor zero", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.Kind = "LOSS"
		row.AmountMinor = 2500

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa: LOSS com valor diferente de zero")
		} else {
			assertConstraint(t, err, "wager_transactions_loss_amount_is_zero")
		}
	})

	t.Run("LOSS com zero é aceito", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.Kind = "LOSS"
		row.AmountMinor = 0

		if err := insertTransaction(ctx, row); err != nil {
			t.Fatalf("LOSS com 0.00 deveria ser aceito: %v", err)
		}
	})

	t.Run("demais tipos exigem valor positivo", func(t *testing.T) {
		for _, kind := range []string{"BET", "WIN"} {
			row := newExternalTransaction(t, wallet)
			row.Kind = kind
			row.AmountMinor = 0

			if err := insertTransaction(ctx, row); err == nil {
				t.Errorf("esperava recusa para %s com valor zero", kind)
			} else {
				assertConstraint(t, err, "wager_transactions_other_kinds_are_positive")
			}
		}
	})

	t.Run("valor negativo é recusado", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.AmountMinor = -1

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa do CHECK de valor não negativo")
		} else {
			assertConstraint(t, err, "wager_transactions_amount_non_negative")
		}
	})
}

// A moeda da movimentação precisa ser a da carteira. A chave estrangeira
// composta impõe isso no banco, sem depender de validação na aplicação.
func TestTransactionCurrencyMustMatchWallet(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)

	row := newExternalTransaction(t, wallet)
	row.Currency = "USD"

	if err := insertTransaction(ctx, row); err == nil {
		t.Fatal("esperava recusa da chave estrangeira composta")
	} else {
		assertConstraint(t, err, "wager_transactions_wallet_currency_fk")
	}
}

func TestTransactionOriginSeparation(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)

	t.Run("abertura interna não carrega metadados externos", func(t *testing.T) {
		mutations := map[string]func(*transactionRow){
			"provedor":   func(r *transactionRow) { r.ProviderID = text("provider-a") },
			"id externo": func(r *transactionRow) { r.ExternalID = text("transaction-123") },
			"chave":      func(r *transactionRow) { r.IdempotencyKey = text("provider-a:transaction-123") },
			"hash":       func(r *transactionRow) { r.PayloadHash = text("abc") },
			"rodada":     func(r *transactionRow) { r.RoundID = text("round-987") },
			"jogo":       func(r *transactionRow) { r.GameID = text("fortune-chimp") },
		}

		for name, mutate := range mutations {
			row := newOpeningTransaction(t, wallet)
			mutate(&row)

			if err := insertTransaction(ctx, row); err == nil {
				t.Errorf("esperava recusa de OPENING com %s", name)
			} else {
				assertConstraint(t, err, "wager_transactions_origin_metadata")
			}
		}
	})

	t.Run("OPENING não pode ter origem externa", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.Kind = "OPENING"

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa de OPENING externo")
		} else {
			assertConstraint(t, err, "wager_transactions_origin_metadata")
		}
	})

	t.Run("operação externa exige provedor, id externo, chave e hash", func(t *testing.T) {
		mutations := map[string]func(*transactionRow){
			"provedor":   func(r *transactionRow) { r.ProviderID = nil },
			"id externo": func(r *transactionRow) { r.ExternalID = nil },
			"chave":      func(r *transactionRow) { r.IdempotencyKey = nil },
			"hash":       func(r *transactionRow) { r.PayloadHash = nil },
			"rodada":     func(r *transactionRow) { r.RoundID = nil },
			"jogo":       func(r *transactionRow) { r.GameID = nil },
		}

		for name, mutate := range mutations {
			row := newExternalTransaction(t, wallet)
			mutate(&row)

			if err := insertTransaction(ctx, row); err == nil {
				t.Errorf("esperava recusa de operação externa sem %s", name)
			} else {
				assertConstraint(t, err, "wager_transactions_origin_metadata")
			}
		}
	})

	// O crédito inicial acontece uma única vez por carteira.
	t.Run("crédito de abertura não se repete", func(t *testing.T) {
		mustInsertTransaction(t, ctx, newOpeningTransaction(t, wallet))

		if err := insertTransaction(ctx, newOpeningTransaction(t, wallet)); err == nil {
			t.Fatal("esperava recusa do segundo OPENING")
		} else {
			assertConstraint(t, err, "wager_transactions_single_opening_per_wallet_uq")
		}
	})
}

func TestTransactionResultAndFailureCodePresence(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)

	t.Run("operação processada exige resultado financeiro", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.ResultBalanceMinor = nil
		row.ResultCurrency = nil
		row.ResultVersion = nil

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa: PROCESSED sem resultado")
		} else {
			assertConstraint(t, err, "wager_transactions_result_presence")
		}
	})

	t.Run("operação não terminal não carrega resultado", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.Status = "PENDING"

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa: PENDING com resultado")
		} else {
			assertConstraint(t, err, "wager_transactions_result_presence")
		}
	})

	t.Run("resultado é gravado por inteiro", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.ResultCurrency = nil

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa: resultado sem moeda")
		} else {
			assertConstraint(t, err, "wager_transactions_result_consistency")
		}
	})

	t.Run("rejeição exige código de falha", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.Status = "REJECTED"
		row.ResultBalanceMinor, row.ResultCurrency, row.ResultVersion = nil, nil, nil

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa: REJECTED sem failureCode")
		} else {
			assertConstraint(t, err, "wager_transactions_failure_code_presence")
		}
	})

	t.Run("operação bem-sucedida não carrega código de falha", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.FailureCode = text("INSUFFICIENT_FUNDS")

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa: PROCESSED com failureCode")
		} else {
			assertConstraint(t, err, "wager_transactions_failure_code_presence")
		}
	})

	t.Run("rejeição com código é aceita", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.Status = "REJECTED"
		row.FailureCode = text("INSUFFICIENT_FUNDS")
		row.ResultBalanceMinor, row.ResultCurrency, row.ResultVersion = nil, nil, nil

		if err := insertTransaction(ctx, row); err != nil {
			t.Fatalf("rejeição com código deveria ser aceita: %v", err)
		}
	})
}

func TestReversalConstraints(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)
	original := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

	newReversal := func(kind string) transactionRow {
		row := newExternalTransaction(t, wallet)
		row.Kind = kind
		row.ReferenceExternalID = original.ExternalID
		row.ReferenceID = &original.ID
		return row
	}

	t.Run("reversão exige referência externa", func(t *testing.T) {
		row := newExternalTransaction(t, wallet)
		row.Kind = "REFUND"

		if err := insertTransaction(ctx, row); err == nil {
			t.Fatal("esperava recusa: REFUND sem referência")
		} else {
			assertConstraint(t, err, "wager_transactions_reversal_requires_reference")
		}
	})

	// Impede devolver o mesmo débito duas vezes.
	t.Run("uma referência recebe só uma reversão bem-sucedida por tipo", func(t *testing.T) {
		mustInsertTransaction(t, ctx, newReversal("REFUND"))

		if err := insertTransaction(ctx, newReversal("REFUND")); err == nil {
			t.Fatal("esperava recusa do segundo REFUND processado")
		} else {
			assertConstraint(t, err, "wager_transactions_single_successful_reversal_uq")
		}
	})

	t.Run("REFUND e ROLLBACK sobre a mesma referência são tipos distintos", func(t *testing.T) {
		if err := insertTransaction(ctx, newReversal("ROLLBACK")); err != nil {
			t.Fatalf("ROLLBACK após REFUND deveria ser permitido pelo schema: %v", err)
		}
	})

	// O índice parcial só considera reversões processadas: uma tentativa
	// rejeitada não bloqueia a reversão legítima que venha depois.
	t.Run("reversão rejeitada não ocupa a vaga", func(t *testing.T) {
		truncateAll(t)
		wallet := mustInsertWallet(t, ctx)
		reference := mustInsertTransaction(t, ctx, newExternalTransaction(t, wallet))

		rejected := newExternalTransaction(t, wallet)
		rejected.Kind = "REFUND"
		rejected.Status = "REJECTED"
		rejected.FailureCode = text("REVERSAL_EXCEEDS_BALANCE")
		rejected.ReferenceExternalID = reference.ExternalID
		rejected.ReferenceID = &reference.ID
		rejected.ResultBalanceMinor, rejected.ResultCurrency, rejected.ResultVersion = nil, nil, nil
		mustInsertTransaction(t, ctx, rejected)

		accepted := newExternalTransaction(t, wallet)
		accepted.Kind = "REFUND"
		accepted.ReferenceExternalID = reference.ExternalID
		accepted.ReferenceID = &reference.ID

		if err := insertTransaction(ctx, accepted); err != nil {
			t.Fatalf("a reversão seguinte deveria ser aceita: %v", err)
		}
	})
}

func TestTransactionRejectsUnknownEnums(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)

	tests := []struct {
		name        string
		mutate      func(*transactionRow)
		constraints []string
	}{
		{
			name:        "tipo desconhecido",
			mutate:      func(r *transactionRow) { r.Kind = "DEPOSIT" },
			constraints: []string{"wager_transactions_kind_valid"},
		},
		{
			// O resultado é limpo junto: um estado não terminal com resultado
			// violaria outra regra antes de o CHECK de estado ser avaliado.
			name: "estado desconhecido",
			mutate: func(r *transactionRow) {
				r.Status = "CANCELLED"
				r.ResultBalanceMinor, r.ResultCurrency, r.ResultVersion = nil, nil, nil
			},
			constraints: []string{"wager_transactions_status_valid"},
		},
		{
			name:   "origem desconhecida",
			mutate: func(r *transactionRow) { r.Origin = "PARTNER" },
			constraints: []string{
				"wager_transactions_origin_valid",
				"wager_transactions_origin_metadata",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := newExternalTransaction(t, wallet)
			tt.mutate(&row)

			if err := insertTransaction(ctx, row); err == nil {
				t.Fatal("esperava recusa do CHECK")
			} else {
				assertConstraintOneOf(t, err, tt.constraints...)
			}
		})
	}
}

func TestTransactionRequiresExistingWallet(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	wallet := mustInsertWallet(t, ctx)

	row := newExternalTransaction(t, wallet)
	row.WalletID = uuid.New()

	if err := insertTransaction(ctx, row); err == nil {
		t.Fatal("esperava recusa da chave estrangeira")
	} else {
		assertConstraint(t, err, "wager_transactions_wallet_currency_fk")
	}
}
