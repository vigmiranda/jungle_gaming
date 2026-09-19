package wagering_test

import (
	"errors"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

var (
	transactionID = mustID("0192f298-345e-7e38-af88-e43f851a819d")
	walletID      = mustID("0192f291-27dd-7d3f-8071-5f8685deef37")
	playerID      = mustID("0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1")
	referenceID   = mustID("0192f2a0-0000-7000-8000-000000000001")
	createdAt     = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	processedAt   = createdAt.Add(time.Second)
)

func mustID(raw string) shared.ID {
	parsed, err := shared.ParseID(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

func externalParams(t *testing.T) wagering.ExternalParams {
	t.Helper()
	return wagering.ExternalParams{
		ID:             transactionID,
		Kind:           wagering.Bet,
		WalletID:       walletID,
		PlayerID:       playerID,
		Amount:         brl(t, "25.00"),
		ProviderID:     "provider-a",
		ExternalID:     "transaction-123",
		IdempotencyKey: "provider-a:transaction-123",
		PayloadHash:    "3f786850e387550fdab836ed7e6dc881de23001b",
		RoundID:        "round-987",
		GameID:         "fortune-chimp",
		CreatedAt:      createdAt,
	}
}

func newExternal(t *testing.T, mutate func(*wagering.ExternalParams)) *wagering.Transaction {
	t.Helper()
	params := externalParams(t)
	if mutate != nil {
		mutate(&params)
	}
	created, err := wagering.NewExternal(params)
	if err != nil {
		t.Fatalf("NewExternal devolveu erro: %v", err)
	}
	return created
}

func result(t *testing.T, balance string, version int64) wagering.Result {
	t.Helper()
	return wagering.Result{Balance: brl(t, balance), WalletVersion: version}
}

func TestNewExternalStartsPending(t *testing.T) {
	created := newExternal(t, nil)

	if created.Status() != wagering.Pending {
		t.Errorf("Status = %q, esperado PENDING", created.Status())
	}
	if !created.ID().Equal(transactionID) || !created.WalletID().Equal(walletID) ||
		!created.PlayerID().Equal(playerID) {
		t.Errorf("identidade = %s / %s / %s", created.ID(), created.WalletID(), created.PlayerID())
	}
	if created.Origin() != wagering.OriginExternal {
		t.Errorf("Origin = %q, esperado EXTERNAL", created.Origin())
	}
	if created.Kind() != wagering.Bet || created.Amount().String() != "25.00" {
		t.Errorf("tipo/valor = %s / %s", created.Kind(), created.Amount())
	}
	if created.ProviderID() != "provider-a" || created.ExternalID() != "transaction-123" {
		t.Errorf("identidade externa = %s / %s", created.ProviderID(), created.ExternalID())
	}
	if created.IdempotencyKey() != "provider-a:transaction-123" || created.PayloadHash() == "" {
		t.Errorf("idempotência = %s / %s", created.IdempotencyKey(), created.PayloadHash())
	}
	if created.RoundID() != "round-987" || created.GameID() != "fortune-chimp" {
		t.Errorf("rodada/jogo = %s / %s", created.RoundID(), created.GameID())
	}
	if created.FailureCode() != "" {
		t.Errorf("FailureCode = %q, esperado vazio", created.FailureCode())
	}
	if _, ok := created.Result(); ok {
		t.Error("não deveria haver resultado antes do processamento")
	}
	if _, ok := created.ReferenceID(); ok {
		t.Error("não deveria haver referência resolvida")
	}
	if !created.CreatedAt().Equal(createdAt) || !created.UpdatedAt().Equal(createdAt) {
		t.Errorf("instantes = %s / %s", created.CreatedAt(), created.UpdatedAt())
	}
}

func TestNewExternalRejectsInvalidInput(t *testing.T) {
	var zeroID shared.ID
	var uninitialized money.Money

	tests := []struct {
		name     string
		mutate   func(*wagering.ExternalParams)
		wantCode error
	}{
		{"sem id", func(p *wagering.ExternalParams) { p.ID = zeroID }, shared.ErrInvalidID},
		{"sem carteira", func(p *wagering.ExternalParams) { p.WalletID = zeroID }, shared.ErrInvalidID},
		{"sem jogador", func(p *wagering.ExternalParams) { p.PlayerID = zeroID }, shared.ErrInvalidID},
		{"tipo interno", func(p *wagering.ExternalParams) { p.Kind = wagering.Opening }, wagering.ErrOpeningNotAllowed},
		{"tipo desconhecido", func(p *wagering.ExternalParams) { p.Kind = "DEPOSIT" }, wagering.ErrUnknownKind},
		{"valor não inicializado", func(p *wagering.ExternalParams) { p.Amount = uninitialized }, money.ErrUninitialized},
		{"BET com valor zero", func(p *wagering.ExternalParams) { p.Amount = brl(t, "0.00") }, wagering.ErrInvalidAmountForKind},
		{"reversão sem referência", func(p *wagering.ExternalParams) { p.Kind = wagering.Refund }, wagering.ErrReferenceRequired},
		{"BET com referência", func(p *wagering.ExternalParams) { p.ReferenceExternalID = "x" }, wagering.ErrReferenceNotAllowed},
		{"sem provedor", func(p *wagering.ExternalParams) { p.ProviderID = "" }, wagering.ErrMissingField},
		{"sem id externo", func(p *wagering.ExternalParams) { p.ExternalID = "" }, wagering.ErrMissingField},
		{"sem chave de idempotência", func(p *wagering.ExternalParams) { p.IdempotencyKey = "" }, wagering.ErrMissingField},
		{"sem hash", func(p *wagering.ExternalParams) { p.PayloadHash = "" }, wagering.ErrMissingField},
		{"sem rodada", func(p *wagering.ExternalParams) { p.RoundID = "" }, wagering.ErrMissingField},
		{"sem jogo", func(p *wagering.ExternalParams) { p.GameID = "" }, wagering.ErrMissingField},
		{"sem instante", func(p *wagering.ExternalParams) { p.CreatedAt = time.Time{} }, wagering.ErrMissingField},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := externalParams(t)
			tt.mutate(&params)

			_, err := wagering.NewExternal(params)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}

func TestNewOpeningCarriesOnlyInternalMetadata(t *testing.T) {
	opening, err := wagering.NewOpening(wagering.OpeningParams{
		ID:        transactionID,
		WalletID:  walletID,
		PlayerID:  playerID,
		Amount:    brl(t, "1000.00"),
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("NewOpening devolveu erro: %v", err)
	}

	if opening.Origin() != wagering.OriginInternal || opening.Kind() != wagering.Opening {
		t.Errorf("origem/tipo = %s / %s", opening.Origin(), opening.Kind())
	}
	if opening.Status() != wagering.Pending {
		t.Errorf("Status = %q, esperado PENDING", opening.Status())
	}
	if opening.ProviderID() != "" || opening.ExternalID() != "" || opening.IdempotencyKey() != "" ||
		opening.PayloadHash() != "" || opening.RoundID() != "" || opening.GameID() != "" {
		t.Error("a abertura interna não deveria carregar metadados externos")
	}
}

// A abertura com crédito nasce concluída: não há estado intermediário
// observável entre criar a carteira e registrar o crédito inicial.
func TestNewProcessedOpeningIsBornProcessed(t *testing.T) {
	opening, err := wagering.NewProcessedOpening(wagering.OpeningParams{
		ID:        transactionID,
		WalletID:  walletID,
		PlayerID:  playerID,
		Amount:    brl(t, "1000.00"),
		CreatedAt: createdAt,
	}, result(t, "1000.00", 1))
	if err != nil {
		t.Fatalf("NewProcessedOpening devolveu erro: %v", err)
	}

	if opening.Status() != wagering.Processed {
		t.Errorf("Status = %q, esperado PROCESSED", opening.Status())
	}
	recorded, ok := opening.Result()
	if !ok || recorded.Balance.String() != "1000.00" || recorded.WalletVersion != 1 {
		t.Errorf("resultado = %+v (%v)", recorded, ok)
	}
}

func TestNewProcessedOpeningRejectsInvalidInput(t *testing.T) {
	valid := wagering.OpeningParams{
		ID: transactionID, WalletID: walletID, PlayerID: playerID,
		Amount: brl(t, "1000.00"), CreatedAt: createdAt,
	}

	t.Run("abertura inválida", func(t *testing.T) {
		params := valid
		params.Amount = brl(t, "0.00")

		_, err := wagering.NewProcessedOpening(params, result(t, "0.00", 1))

		if !errors.Is(err, wagering.ErrInvalidAmountForKind) {
			t.Errorf("erro = %v, esperado ErrInvalidAmountForKind", err)
		}
	})

	t.Run("resultado inválido", func(t *testing.T) {
		_, err := wagering.NewProcessedOpening(valid, result(t, "1000.00", 0))

		if !errors.Is(err, wagering.ErrInvalidTransactionState) {
			t.Errorf("erro = %v, esperado ErrInvalidTransactionState", err)
		}
	})
}

func TestNewOpeningRejectsInvalidInput(t *testing.T) {
	var zeroID shared.ID
	var uninitialized money.Money

	valid := wagering.OpeningParams{
		ID: transactionID, WalletID: walletID, PlayerID: playerID,
		Amount: brl(t, "1000.00"), CreatedAt: createdAt,
	}

	tests := []struct {
		name     string
		mutate   func(*wagering.OpeningParams)
		wantCode error
	}{
		{"sem id", func(p *wagering.OpeningParams) { p.ID = zeroID }, shared.ErrInvalidID},
		{"sem carteira", func(p *wagering.OpeningParams) { p.WalletID = zeroID }, shared.ErrInvalidID},
		{"sem jogador", func(p *wagering.OpeningParams) { p.PlayerID = zeroID }, shared.ErrInvalidID},
		{"valor não inicializado", func(p *wagering.OpeningParams) { p.Amount = uninitialized }, money.ErrUninitialized},
		{"valor zero", func(p *wagering.OpeningParams) { p.Amount = brl(t, "0.00") }, wagering.ErrInvalidAmountForKind},
		{"sem instante", func(p *wagering.OpeningParams) { p.CreatedAt = time.Time{} }, wagering.ErrMissingField},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := valid
			tt.mutate(&params)

			_, err := wagering.NewOpening(params)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}

func TestMarkProcessedRecordsResultForReplay(t *testing.T) {
	transaction := newExternal(t, nil)

	if err := transaction.MarkProcessed(result(t, "975.00", 2), processedAt); err != nil {
		t.Fatalf("MarkProcessed devolveu erro: %v", err)
	}

	if transaction.Status() != wagering.Processed {
		t.Errorf("Status = %q, esperado PROCESSED", transaction.Status())
	}
	if !transaction.UpdatedAt().Equal(processedAt) {
		t.Errorf("UpdatedAt = %s, esperado %s", transaction.UpdatedAt(), processedAt)
	}

	// O saldo fica registrado na transação: o replay devolve o valor observado
	// no processamento original, não o saldo atual da carteira (ADR-014).
	recorded, ok := transaction.Result()
	if !ok {
		t.Fatal("esperava resultado registrado")
	}
	if recorded.Balance.String() != "975.00" || recorded.WalletVersion != 2 {
		t.Errorf("resultado = %+v", recorded)
	}
}

func TestMarkProcessedRejectsInvalidResult(t *testing.T) {
	var uninitialized money.Money

	tests := []struct {
		name     string
		result   wagering.Result
		wantCode error
	}{
		{"saldo não inicializado", wagering.Result{Balance: uninitialized, WalletVersion: 2}, money.ErrUninitialized},
		{"versão zero", result(t, "975.00", 0), wagering.ErrInvalidTransactionState},
		{"versão negativa", result(t, "975.00", -1), wagering.ErrInvalidTransactionState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transaction := newExternal(t, nil)

			err := transaction.MarkProcessed(tt.result, processedAt)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
			if transaction.Status() != wagering.Pending {
				t.Errorf("Status = %q, a recusa não deveria transicionar", transaction.Status())
			}
		})
	}
}

func TestMarkPendingReferenceOnlyForReversals(t *testing.T) {
	reversal := newExternal(t, func(p *wagering.ExternalParams) {
		p.Kind = wagering.Refund
		p.ReferenceExternalID = "transaction-123"
	})

	if err := reversal.MarkPendingReference(processedAt); err != nil {
		t.Fatalf("MarkPendingReference devolveu erro: %v", err)
	}
	if reversal.Status() != wagering.PendingReference {
		t.Errorf("Status = %q, esperado PENDING_REFERENCE", reversal.Status())
	}
	next, ok := reversal.NextRetryAt()
	if !ok || !next.Equal(processedAt) {
		t.Errorf("NextRetryAt = %v (%v), esperado %s", next, ok, processedAt)
	}
	if reversal.AttemptCount() != 0 {
		t.Errorf("AttemptCount = %d, esperado 0", reversal.AttemptCount())
	}

	bet := newExternal(t, nil)
	if err := bet.MarkPendingReference(processedAt); !errors.Is(err, wagering.ErrIllegalTransition) {
		t.Errorf("erro = %v, esperado ErrIllegalTransition para BET", err)
	}
}

func TestScheduleReferenceRetry(t *testing.T) {
	reversal := newExternal(t, func(p *wagering.ExternalParams) {
		p.Kind = wagering.Refund
		p.ReferenceExternalID = "transaction-123"
	})
	if err := reversal.MarkPendingReference(processedAt); err != nil {
		t.Fatal(err)
	}

	next := processedAt.Add(time.Second)
	if err := reversal.ScheduleReferenceRetry(3, next, next); err != nil {
		t.Fatal(err)
	}
	if reversal.AttemptCount() != 3 {
		t.Errorf("attempts = %d", reversal.AttemptCount())
	}
	got, ok := reversal.NextRetryAt()
	if !ok || !got.Equal(next) {
		t.Errorf("next = %v (%v)", got, ok)
	}

	if err := reversal.ScheduleReferenceRetry(-1, next, next); !errors.Is(err, wagering.ErrInvalidTransactionState) {
		t.Errorf("attempts negativos: %v", err)
	}
	if err := reversal.ScheduleReferenceRetry(1, time.Time{}, next); !errors.Is(err, wagering.ErrMissingField) {
		t.Errorf("next zero: %v", err)
	}

	bet := newExternal(t, nil)
	if err := bet.ScheduleReferenceRetry(1, next, next); !errors.Is(err, wagering.ErrIllegalTransition) {
		t.Errorf("erro = %v", err)
	}
}

func TestPendingReferenceResolvesLater(t *testing.T) {
	reversal := newExternal(t, func(p *wagering.ExternalParams) {
		p.Kind = wagering.Rollback
		p.ReferenceExternalID = "transaction-123"
	})
	if err := reversal.MarkPendingReference(processedAt); err != nil {
		t.Fatalf("MarkPendingReference devolveu erro: %v", err)
	}

	resolvedAt := processedAt.Add(time.Minute)
	if err := reversal.ResolveReference(referenceID, resolvedAt); err != nil {
		t.Fatalf("ResolveReference devolveu erro: %v", err)
	}

	resolved, ok := reversal.ReferenceID()
	if !ok || !resolved.Equal(referenceID) {
		t.Errorf("referência resolvida = %s (%v)", resolved, ok)
	}
	if !reversal.UpdatedAt().Equal(resolvedAt) {
		t.Errorf("UpdatedAt = %s, esperado %s", reversal.UpdatedAt(), resolvedAt)
	}

	if err := reversal.MarkProcessed(result(t, "1000.00", 3), resolvedAt); err != nil {
		t.Fatalf("MarkProcessed devolveu erro: %v", err)
	}
	if reversal.Status() != wagering.Processed {
		t.Errorf("Status = %q, esperado PROCESSED", reversal.Status())
	}
	if _, ok := reversal.NextRetryAt(); ok {
		t.Error("next_retry_at deveria ser limpo no terminal")
	}
}

func TestResolveReferenceRejectsInvalidUse(t *testing.T) {
	var zeroID shared.ID

	t.Run("tipo sem referência", func(t *testing.T) {
		bet := newExternal(t, nil)

		if err := bet.ResolveReference(referenceID, processedAt); !errors.Is(err, wagering.ErrReferenceNotAllowed) {
			t.Errorf("erro = %v, esperado ErrReferenceNotAllowed", err)
		}
	})

	t.Run("referência inválida", func(t *testing.T) {
		reversal := newExternal(t, func(p *wagering.ExternalParams) {
			p.Kind = wagering.Refund
			p.ReferenceExternalID = "transaction-123"
		})

		if err := reversal.ResolveReference(zeroID, processedAt); !errors.Is(err, shared.ErrInvalidID) {
			t.Errorf("erro = %v, esperado ErrInvalidID", err)
		}
	})

	t.Run("sem instante", func(t *testing.T) {
		reversal := newExternal(t, func(p *wagering.ExternalParams) {
			p.Kind = wagering.Refund
			p.ReferenceExternalID = "transaction-123"
		})

		if err := reversal.ResolveReference(referenceID, time.Time{}); !errors.Is(err, wagering.ErrMissingField) {
			t.Errorf("erro = %v, esperado ErrMissingField", err)
		}
	})

	t.Run("transação terminal", func(t *testing.T) {
		reversal := newExternal(t, func(p *wagering.ExternalParams) {
			p.Kind = wagering.Refund
			p.ReferenceExternalID = "transaction-123"
		})
		if err := reversal.Reject(wagering.FailureReferenceNotFound, processedAt); err != nil {
			t.Fatalf("Reject devolveu erro: %v", err)
		}

		if err := reversal.ResolveReference(referenceID, processedAt); !errors.Is(err, wagering.ErrIllegalTransition) {
			t.Errorf("erro = %v, esperado ErrIllegalTransition", err)
		}
	})
}

func TestRejectAndFailRecordCode(t *testing.T) {
	rejected := newExternal(t, nil)
	if err := rejected.Reject(wagering.FailureInsufficientFunds, processedAt); err != nil {
		t.Fatalf("Reject devolveu erro: %v", err)
	}
	if rejected.Status() != wagering.Rejected || rejected.FailureCode() != wagering.FailureInsufficientFunds {
		t.Errorf("estado/código = %s / %s", rejected.Status(), rejected.FailureCode())
	}

	failed := newExternal(t, nil)
	if err := failed.Fail(wagering.FailureInfrastructure, processedAt); err != nil {
		t.Fatalf("Fail devolveu erro: %v", err)
	}
	if failed.Status() != wagering.Failed || failed.FailureCode() != wagering.FailureInfrastructure {
		t.Errorf("estado/código = %s / %s", failed.Status(), failed.FailureCode())
	}
}

func TestRejectAndFailRequireCode(t *testing.T) {
	transaction := newExternal(t, nil)

	if err := transaction.Reject("", processedAt); !errors.Is(err, wagering.ErrMissingField) {
		t.Errorf("Reject sem código: erro = %v, esperado ErrMissingField", err)
	}
	if err := transaction.Fail("", processedAt); !errors.Is(err, wagering.ErrMissingField) {
		t.Errorf("Fail sem código: erro = %v, esperado ErrMissingField", err)
	}
	if transaction.Status() != wagering.Pending {
		t.Errorf("Status = %q, a recusa não deveria transicionar", transaction.Status())
	}
}

func TestTransitionsRequireTimestamp(t *testing.T) {
	transaction := newExternal(t, nil)

	if err := transaction.MarkProcessed(result(t, "975.00", 2), time.Time{}); !errors.Is(err, wagering.ErrMissingField) {
		t.Errorf("erro = %v, esperado ErrMissingField", err)
	}
	if _, ok := transaction.Result(); ok {
		t.Error("o resultado não deveria ser registrado quando a transição falha")
	}
}

// Uma transação terminal não sofre novas transições: o replay consulta o
// resultado persistido em vez de reaplicar a operação.
func TestTerminalStatesRefuseFurtherTransitions(t *testing.T) {
	terminals := []struct {
		name  string
		build func(*testing.T) *wagering.Transaction
	}{
		{"processada", func(t *testing.T) *wagering.Transaction {
			transaction := newExternal(t, nil)
			if err := transaction.MarkProcessed(result(t, "975.00", 2), processedAt); err != nil {
				t.Fatalf("MarkProcessed devolveu erro: %v", err)
			}
			return transaction
		}},
		{"rejeitada", func(t *testing.T) *wagering.Transaction {
			transaction := newExternal(t, nil)
			if err := transaction.Reject(wagering.FailureInsufficientFunds, processedAt); err != nil {
				t.Fatalf("Reject devolveu erro: %v", err)
			}
			return transaction
		}},
		{"falha permanente", func(t *testing.T) *wagering.Transaction {
			transaction := newExternal(t, nil)
			if err := transaction.Fail(wagering.FailureInfrastructure, processedAt); err != nil {
				t.Fatalf("Fail devolveu erro: %v", err)
			}
			return transaction
		}},
	}

	later := processedAt.Add(time.Hour)
	for _, terminal := range terminals {
		t.Run(terminal.name, func(t *testing.T) {
			transaction := terminal.build(t)
			statusBefore := transaction.Status()

			if err := transaction.MarkProcessed(result(t, "0.00", 3), later); !errors.Is(err, wagering.ErrIllegalTransition) {
				t.Errorf("MarkProcessed: erro = %v, esperado ErrIllegalTransition", err)
			}
			if err := transaction.Reject(wagering.FailureInvalidAmount, later); !errors.Is(err, wagering.ErrIllegalTransition) {
				t.Errorf("Reject: erro = %v, esperado ErrIllegalTransition", err)
			}
			if err := transaction.Fail(wagering.FailureInfrastructure, later); !errors.Is(err, wagering.ErrIllegalTransition) {
				t.Errorf("Fail: erro = %v, esperado ErrIllegalTransition", err)
			}
			if transaction.Status() != statusBefore {
				t.Errorf("Status = %q, esperado permanecer %q", transaction.Status(), statusBefore)
			}
		})
	}
}

func TestPendingReferenceCannotRepeat(t *testing.T) {
	reversal := newExternal(t, func(p *wagering.ExternalParams) {
		p.Kind = wagering.Refund
		p.ReferenceExternalID = "transaction-123"
	})
	if err := reversal.MarkPendingReference(processedAt); err != nil {
		t.Fatalf("MarkPendingReference devolveu erro: %v", err)
	}

	err := reversal.MarkPendingReference(processedAt.Add(time.Minute))

	if !errors.Is(err, wagering.ErrIllegalTransition) {
		t.Errorf("erro = %v, esperado ErrIllegalTransition", err)
	}
}
