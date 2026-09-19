// Package correlation transporta o identificador de rastreio entre a borda,
// os casos de uso e os eventos publicados (ADR-017).
package correlation

import (
	"context"

	"github.com/google/uuid"
)

// Header é o cabeçalho HTTP usado para receber ou devolver o identificador.
const Header = "X-Correlation-Id"

type contextKey struct{}

// NewID gera um identificador novo de rastreio.
func NewID() string {
	return uuid.NewString()
}

// WithID associa o identificador ao contexto.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext devolve o identificador associado ao contexto, ou vazio.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
