package httpserver

import (
	"errors"
	"net/http"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

func TestDecodeLedgerCursorRejectsInvalid(t *testing.T) {
	_, err := decodeLedgerCursor("%%%")
	if err == nil {
		t.Fatal("esperava erro")
	}
	status, body := mapError(err)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", status)
	}
	if body.Error != "invalid_cursor" {
		t.Fatalf("error = %q", body.Error)
	}
}

func TestDecodeLedgerCursorAcceptsEmpty(t *testing.T) {
	cursor, err := decodeLedgerCursor("")
	if err != nil {
		t.Fatal(err)
	}
	if cursor != nil {
		t.Fatal("cursor vazio deveria ser nil")
	}
}

func TestMapErrorDistinguishesBusinessRejectionFromInfra(t *testing.T) {
	// REJECTED (negócio) → 422 com failureCode; erro genérico de infra → 503.
	rejected := shared.Rejection(string(wagering.FailureInsufficientFunds), "sem saldo")
	status, body := mapError(rejected)
	if status != http.StatusUnprocessableEntity || body.FailureCode != string(wagering.FailureInsufficientFunds) {
		t.Fatalf("REJECTED: status=%d body=%+v", status, body)
	}

	status, _ = mapError(errors.New("postgres connection reset"))
	if status != http.StatusServiceUnavailable {
		t.Fatalf("infra: status=%d", status)
	}
}
