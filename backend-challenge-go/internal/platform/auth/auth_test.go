package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/vigmi/backend-challenge-go/internal/config"
)

func TestJWKSValidatorAcceptsProviderToken(t *testing.T) {
	t.Parallel()

	bundle := newTestKeyBundle(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(bundle.jwks())
	}))
	t.Cleanup(server.Close)

	validator := NewJWKSValidator(config.Config{
		OIDC: config.OIDC{
			IssuerURL:   "http://issuer.test/realms/wagering",
			Audience:    "wagering-api",
			JWKSURL:     server.URL,
			JWKSRefresh: time.Minute,
		},
	})

	token := bundle.sign(t, oidcClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://issuer.test/realms/wagering",
			Subject:   "provider-a",
			Audience:  []string{"wagering-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		ProviderID: "provider-a",
		WalletRole: string(RoleProvider),
		ClientID:   "provider-a",
	})

	identity, err := validator.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if identity.Role != RoleProvider || identity.ProviderID != "provider-a" {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestJWKSValidatorRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	bundle := newTestKeyBundle(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(bundle.jwks())
	}))
	t.Cleanup(server.Close)

	validator := NewJWKSValidator(config.Config{
		OIDC: config.OIDC{
			IssuerURL:   "http://issuer.test/realms/wagering",
			Audience:    "wagering-api",
			JWKSURL:     server.URL,
			JWKSRefresh: time.Minute,
		},
	})

	token := bundle.sign(t, oidcClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "http://issuer.test/realms/wagering",
			Subject:   "provider-a",
			Audience:  []string{"wagering-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)),
		},
		ProviderID: "provider-a",
		WalletRole: string(RoleProvider),
	})

	if _, err := validator.Validate(context.Background(), token); err == nil {
		t.Fatal("esperava rejeitar token expirado")
	}
}

func TestAuthenticatorMiddlewareRequiresBearer(t *testing.T) {
	t.Parallel()

	authenticator := NewAuthenticator(staticValidator{
		identity: Identity{Role: RoleInternal, Subject: "internal"},
	})

	called := false
	handler := authenticator.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wallets", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", rec.Code)
	}
	if called {
		t.Fatal("handler não deveria ser chamado sem token")
	}
}

func TestRequireInternalRejectsProvider(t *testing.T) {
	t.Parallel()

	called := false
	handler := RequireInternal(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	req = req.WithContext(WithIdentity(req.Context(), Identity{
		Role:       RoleProvider,
		ProviderID: "provider-a",
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", rec.Code)
	}
	if called {
		t.Fatal("handler não deveria ser chamado")
	}
}

type staticValidator struct {
	identity Identity
	err      error
}

func (v staticValidator) Validate(context.Context, string) (Identity, error) {
	if v.err != nil {
		return Identity{}, v.err
	}
	return v.identity, nil
}

type testKeyBundle struct {
	key *rsa.PrivateKey
	kid string
}

func newTestKeyBundle(t *testing.T) testKeyBundle {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gerar chave: %v", err)
	}
	return testKeyBundle{key: key, kid: "test-key"}
}

func (b testKeyBundle) jwks() jwksDocument {
	return jwksDocument{Keys: []jwkKey{{
		Kid: b.kid,
		Kty: "RSA",
		Alg: "RS256",
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(b.key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(b.key.E)).Bytes()),
	}}}
}

func (b testKeyBundle) sign(t *testing.T, claims oidcClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = b.kid
	signed, err := token.SignedString(b.key)
	if err != nil {
		t.Fatalf("assinar: %v", err)
	}
	return signed
}
