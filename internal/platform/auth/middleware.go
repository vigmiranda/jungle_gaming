package auth

import (
	"net/http"
	"strings"
)

// Authenticator expõe middlewares HTTP de autenticação e autorização.
type Authenticator struct {
	validator Validator
}

// NewAuthenticator monta o middleware a partir do validador OIDC.
func NewAuthenticator(validator Validator) *Authenticator {
	return &Authenticator{validator: validator}
}

// Middleware exige Bearer JWT válido e grava a identidade no contexto.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "token ausente ou malformado")
			return
		}

		identity, err := a.validator.Validate(r.Context(), raw)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "token inválido ou expirado")
			return
		}

		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
	})
}

// RequireInternal restringe a rota ao serviço interno.
func RequireInternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := RequireIdentity(r.Context())
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "autenticação obrigatória")
			return
		}
		if identity.Role != RoleInternal {
			writeAuthError(w, http.StatusForbidden, "forbidden", "operação restrita ao serviço interno")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireProvider restringe a rota a um client de provedor.
func RequireProvider(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := RequireIdentity(r.Context())
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "autenticação obrigatória")
			return
		}
		if identity.Role != RoleProvider || identity.ProviderID == "" {
			writeAuthError(w, http.StatusForbidden, "forbidden", "operação restrita a provedores")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
