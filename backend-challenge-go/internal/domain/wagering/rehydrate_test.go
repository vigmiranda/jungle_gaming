package wagering_test

import (
	"errors"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

func externalState(t *testing.T) wagering.State {
	t.Helper()
	return wagering.State{
		ID:             transactionID,
		Origin:         wagering.OriginExternal,
		Kind:           wagering.Bet,
		WalletID:       walletID,
		PlayerID:       playerID,
		Amount:         brl(t, "25.00"),
		Status:         wagering.Processed,
		ProviderID:     "provider-a",
		ExternalID:     "transaction-123",
		IdempotencyKey: "provider-a:transaction-123",
		PayloadHash:    "3f786850e387550fdab836ed7e6dc881de23001b",
		RoundID:        "round-987",
		GameID:         "fortune-chimp",
		Result:         &wagering.Result{Balance: brl(t, "975.00"), WalletVersion: 2},
		CreatedAt:      createdAt,
		UpdatedAt:      processedAt,
	}
}

func TestRehydrateRestoresPendingRetryMetadata(t *testing.T) {
	next := processedAt.Add(time.Minute)
	state := externalState(t)
	state.Kind = wagering.Rollback
	state.Status = wagering.PendingReference
	state.ReferenceExternalID = "transaction-123"
	state.Result = nil
	state.AttemptCount = 4
	state.NextRetryAt = &next

	restored, err := wagering.Rehydrate(state)
	if err != nil {
		t.Fatal(err)
	}
	if restored.AttemptCount() != 4 {
		t.Errorf("attempts = %d", restored.AttemptCount())
	}
	got, ok := restored.NextRetryAt()
	if !ok || !got.Equal(next) {
		t.Errorf("next = %v (%v)", got, ok)
	}
}

func TestRehydrateRestoresExternalTransaction(t *testing.T) {
	restored, err := wagering.Rehydrate(externalState(t))
	if err != nil {
		t.Fatalf("Rehydrate devolveu erro: %v", err)
	}

	if restored.Status() != wagering.Processed {
		t.Errorf("Status = %q, esperado PROCESSED", restored.Status())
	}
	if restored.Origin() != wagering.OriginExternal || restored.Kind() != wagering.Bet {
		t.Errorf("origem/tipo = %s / %s", restored.Origin(), restored.Kind())
	}

	// A reidratação não reaplica a movimentação: o resultado vem do banco.
	recorded, ok := restored.Result()
	if !ok || recorded.Balance.String() != "975.00" || recorded.WalletVersion != 2 {
		t.Errorf("resultado = %+v (%v)", recorded, ok)
	}
}

func TestRehydrateRestoresOpening(t *testing.T) {
	restored, err := wagering.Rehydrate(wagering.State{
		ID:        transactionID,
		Origin:    wagering.OriginInternal,
		Kind:      wagering.Opening,
		WalletID:  walletID,
		PlayerID:  playerID,
		Amount:    brl(t, "1000.00"),
		Status:    wagering.Processed,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("Rehydrate devolveu erro: %v", err)
	}

	if restored.Origin() != wagering.OriginInternal || restored.Kind() != wagering.Opening {
		t.Errorf("origem/tipo = %s / %s", restored.Origin(), restored.Kind())
	}
}

func TestRehydrateRestoresResolvedReference(t *testing.T) {
	state := externalState(t)
	state.Kind = wagering.Refund
	state.ReferenceExternalID = "transaction-123"
	state.ReferenceID = &referenceID

	restored, err := wagering.Rehydrate(state)
	if err != nil {
		t.Fatalf("Rehydrate devolveu erro: %v", err)
	}

	resolved, ok := restored.ReferenceID()
	if !ok || !resolved.Equal(referenceID) {
		t.Errorf("referência = %s (%v)", resolved, ok)
	}
	if restored.ReferenceExternalID() != "transaction-123" {
		t.Errorf("ReferenceExternalID = %q", restored.ReferenceExternalID())
	}
}

func TestRehydrateRestoresFailureCode(t *testing.T) {
	state := externalState(t)
	state.Status = wagering.Rejected
	state.Result = nil
	state.FailureCode = wagering.FailureInsufficientFunds

	restored, err := wagering.Rehydrate(state)
	if err != nil {
		t.Fatalf("Rehydrate devolveu erro: %v", err)
	}

	if restored.FailureCode() != wagering.FailureInsufficientFunds {
		t.Errorf("FailureCode = %q", restored.FailureCode())
	}
	if _, ok := restored.Result(); ok {
		t.Error("uma rejeição não deveria carregar resultado financeiro")
	}
}

func TestRehydrateRejectsInconsistentState(t *testing.T) {
	var zeroID shared.ID
	var uninitialized money.Money

	tests := []struct {
		name     string
		mutate   func(*wagering.State)
		wantCode error
	}{
		{"sem id", func(s *wagering.State) { s.ID = zeroID }, shared.ErrInvalidID},
		{"sem carteira", func(s *wagering.State) { s.WalletID = zeroID }, shared.ErrInvalidID},
		{"sem jogador", func(s *wagering.State) { s.PlayerID = zeroID }, shared.ErrInvalidID},
		{"tipo desconhecido", func(s *wagering.State) { s.Kind = "DEPOSIT" }, wagering.ErrUnknownKind},
		{"estado desconhecido", func(s *wagering.State) { s.Status = "CANCELLED" }, wagering.ErrUnknownStatus},
		{"valor não inicializado", func(s *wagering.State) { s.Amount = uninitialized }, money.ErrUninitialized},
		{"origem desconhecida", func(s *wagering.State) { s.Origin = "PARTNER" }, wagering.ErrInvalidTransactionState},
		{"sem criação", func(s *wagering.State) { s.CreatedAt = time.Time{} }, wagering.ErrInvalidTransactionState},
		{"sem atualização", func(s *wagering.State) { s.UpdatedAt = time.Time{} }, wagering.ErrInvalidTransactionState},
		{"externa sem provedor", func(s *wagering.State) { s.ProviderID = "" }, wagering.ErrInvalidTransactionState},
		{"externa sem id externo", func(s *wagering.State) { s.ExternalID = "" }, wagering.ErrInvalidTransactionState},
		{"externa sem chave", func(s *wagering.State) { s.IdempotencyKey = "" }, wagering.ErrInvalidTransactionState},
		{"externa sem hash", func(s *wagering.State) { s.PayloadHash = "" }, wagering.ErrInvalidTransactionState},
		{"OPENING com origem externa", func(s *wagering.State) { s.Kind = wagering.Opening }, wagering.ErrInvalidTransactionState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := externalState(t)
			tt.mutate(&state)

			_, err := wagering.Rehydrate(state)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}

// O schema distingue origem interna de externa; a reidratação impõe a mesma
// separação para que dados corrompidos não virem uma transação híbrida.
func TestRehydrateRejectsInternalOriginWithExternalMetadata(t *testing.T) {
	base := wagering.State{
		ID:        transactionID,
		Origin:    wagering.OriginInternal,
		Kind:      wagering.Opening,
		WalletID:  walletID,
		PlayerID:  playerID,
		Amount:    brl(t, "1000.00"),
		Status:    wagering.Processed,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}

	tests := []struct {
		name   string
		mutate func(*wagering.State)
	}{
		{"com provedor", func(s *wagering.State) { s.ProviderID = "provider-a" }},
		{"com id externo", func(s *wagering.State) { s.ExternalID = "transaction-123" }},
		{"com chave", func(s *wagering.State) { s.IdempotencyKey = "provider-a:transaction-123" }},
		{"com hash", func(s *wagering.State) { s.PayloadHash = "abc" }},
		{"com rodada", func(s *wagering.State) { s.RoundID = "round-987" }},
		{"com jogo", func(s *wagering.State) { s.GameID = "fortune-chimp" }},
		{"com referência", func(s *wagering.State) { s.ReferenceExternalID = "transaction-123" }},
		{"tipo externo", func(s *wagering.State) { s.Kind = wagering.Bet }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := base
			tt.mutate(&state)

			_, err := wagering.Rehydrate(state)

			if !errors.Is(err, wagering.ErrInvalidTransactionState) {
				t.Errorf("erro = %v, esperado ErrInvalidTransactionState", err)
			}
		})
	}
}
