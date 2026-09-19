// Package auth valida tokens OIDC e aplica autorização por identidade.
//
// A autenticação é responsabilidade da borda: o domínio recebe apenas o
// `providerId` já autorizado, sem conhecer JWT nem Keycloak.
package auth

import (
	"context"
	"errors"
)

// Role classifica a identidade autenticada.
type Role string

// Papéis reconhecidos nas claims do realm.
const (
	// RoleProvider identifica um client de provedor de jogos.
	RoleProvider Role = "provider"
	// RoleInternal identifica o serviço interno de carteira.
	RoleInternal Role = "internal"
)

// Identity é a identidade autenticada extraída do JWT.
type Identity struct {
	Subject    string
	Role       Role
	ProviderID string
	ClientID   string
}

// ErrUnauthenticated cobre token ausente, inválido ou expirado.
var ErrUnauthenticated = errors.New("não autenticado")

// ErrForbidden cobre identidade autenticada sem permissão para a operação.
var ErrForbidden = errors.New("não autorizado")

type contextKey struct{}

// WithIdentity guarda a identidade no contexto da requisição.
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, identity)
}

// FromContext devolve a identidade autenticada, se houver.
func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(contextKey{}).(Identity)
	return identity, ok
}

// RequireIdentity devolve a identidade ou ErrUnauthenticated.
func RequireIdentity(ctx context.Context) (Identity, error) {
	identity, ok := FromContext(ctx)
	if !ok {
		return Identity{}, ErrUnauthenticated
	}
	return identity, nil
}
