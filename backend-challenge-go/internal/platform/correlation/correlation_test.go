package correlation_test

import (
	"context"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
)

func TestFromContextReturnsStoredID(t *testing.T) {
	ctx := correlation.WithID(context.Background(), "abc-123")

	if got := correlation.FromContext(ctx); got != "abc-123" {
		t.Errorf("FromContext = %q, esperado \"abc-123\"", got)
	}
}

func TestFromContextWithoutIDReturnsEmpty(t *testing.T) {
	if got := correlation.FromContext(context.Background()); got != "" {
		t.Errorf("FromContext = %q, esperado vazio", got)
	}
}

func TestNewIDGeneratesDistinctValues(t *testing.T) {
	first, second := correlation.NewID(), correlation.NewID()

	if first == "" || second == "" {
		t.Fatal("NewID não deve devolver vazio")
	}
	if first == second {
		t.Errorf("NewID devolveu o mesmo identificador duas vezes: %q", first)
	}
}
