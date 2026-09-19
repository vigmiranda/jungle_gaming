package shared_test

import (
	"errors"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

func TestNewIDGeneratesDistinctValidIDs(t *testing.T) {
	first, err := shared.NewID()
	if err != nil {
		t.Fatalf("NewID devolveu erro: %v", err)
	}
	second, err := shared.NewID()
	if err != nil {
		t.Fatalf("NewID devolveu erro: %v", err)
	}

	if first.IsZero() || second.IsZero() {
		t.Error("NewID não deveria gerar identificador nulo")
	}
	if first.Equal(second) {
		t.Errorf("NewID gerou o mesmo identificador duas vezes: %s", first)
	}
	if err := first.Validate(); err != nil {
		t.Errorf("Validate devolveu erro: %v", err)
	}
}

func TestParseIDAcceptsCanonicalUUID(t *testing.T) {
	const raw = "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"

	parsed, err := shared.ParseID(raw)
	if err != nil {
		t.Fatalf("ParseID devolveu erro: %v", err)
	}
	if parsed.String() != raw {
		t.Errorf("String = %q, esperado %q", parsed.String(), raw)
	}
}

func TestParseIDRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"vazio", ""},
		{"texto livre", "carteira-1"},
		{"truncado", "0192f28f-5dc0-7d58-bdb2"},
		{"uuid nulo", "00000000-0000-0000-0000-000000000000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := shared.ParseID(tt.raw)

			if !errors.Is(err, shared.ErrInvalidID) {
				t.Errorf("erro = %v, esperado ErrInvalidID", err)
			}
		})
	}
}

func TestZeroIDIsRejected(t *testing.T) {
	var id shared.ID

	if !id.IsZero() {
		t.Error("o valor zero deveria ser considerado não inicializado")
	}
	if id.String() != "" {
		t.Errorf("String = %q, esperado vazio", id.String())
	}
	if err := id.Validate(); !errors.Is(err, shared.ErrInvalidID) {
		t.Errorf("erro = %v, esperado ErrInvalidID", err)
	}
}
