package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/vigmi/backend-challenge-go/internal/config"
)

// Validator valida access tokens emitidos pelo IdP.
type Validator interface {
	Validate(ctx context.Context, rawToken string) (Identity, error)
}

// JWKSValidator busca chaves no endpoint JWKS e valida assinatura e claims.
type JWKSValidator struct {
	issuer   string
	audience string
	jwksURL  string
	client   *http.Client
	refresh  time.Duration

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// NewJWKSValidator monta o validador a partir da configuração OIDC.
func NewJWKSValidator(cfg config.Config) *JWKSValidator {
	return &JWKSValidator{
		issuer:   strings.TrimRight(cfg.OIDC.IssuerURL, "/"),
		audience: cfg.OIDC.Audience,
		jwksURL:  cfg.OIDC.JWKSURL,
		client:   &http.Client{Timeout: 10 * time.Second},
		refresh:  cfg.OIDC.JWKSRefresh,
		keys:     make(map[string]*rsa.PublicKey),
	}
}

type oidcClaims struct {
	jwt.RegisteredClaims
	ProviderID string `json:"provider_id"`
	WalletRole string `json:"wallet_role"`
	ClientID   string `json:"azp"`
}

// Validate verifica assinatura, issuer, audience, expiração e claims de papel.
func (v *JWKSValidator) Validate(ctx context.Context, rawToken string) (Identity, error) {
	if strings.TrimSpace(rawToken) == "" {
		return Identity{}, ErrUnauthenticated
	}

	claims := &oidcClaims{}
	token, err := jwt.ParseWithClaims(rawToken, claims, v.keyFunc, jwt.WithAudience(v.audience),
		jwt.WithIssuer(v.issuer), jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Name}),
		jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return Identity{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}

	role := Role(claims.WalletRole)
	switch role {
	case RoleProvider:
		if claims.ProviderID == "" {
			return Identity{}, fmt.Errorf("%w: provider_id ausente", ErrUnauthenticated)
		}
	case RoleInternal:
		// serviço interno não carrega provider_id
	default:
		return Identity{}, fmt.Errorf("%w: wallet_role inválido", ErrUnauthenticated)
	}

	subject := claims.Subject
	if subject == "" {
		subject = claims.ClientID
	}

	return Identity{
		Subject:    subject,
		Role:       role,
		ProviderID: claims.ProviderID,
		ClientID:   claims.ClientID,
	}, nil
}

func (v *JWKSValidator) keyFunc(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("token sem kid")
	}

	key, err := v.lookup(kid)
	if err == nil {
		return key, nil
	}

	if refreshErr := v.refreshKeys(context.Background()); refreshErr != nil {
		return nil, refreshErr
	}
	return v.lookup(kid)
}

func (v *JWKSValidator) lookup(kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	key, ok := v.keys[kid]
	if !ok {
		return nil, fmt.Errorf("chave %q ausente no JWKS", kid)
	}
	if v.refresh > 0 && !v.fetchedAt.IsZero() && time.Since(v.fetchedAt) > v.refresh {
		return nil, errors.New("cache JWKS expirado")
	}
	return key, nil
}

type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (v *JWKSValidator) refreshKeys(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.refresh > 0 && !v.fetchedAt.IsZero() && time.Since(v.fetchedAt) < v.refresh && len(v.keys) > 0 {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("jwks: montar requisição: %w", err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("jwks: buscar chaves: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks: status %d", resp.StatusCode)
	}

	var doc jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("jwks: decodificar: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, item := range doc.Keys {
		if item.Kty != "RSA" || item.Kid == "" {
			continue
		}
		if item.Use != "" && item.Use != "sig" {
			continue
		}
		pub, err := parseRSAPublicKey(item.N, item.E)
		if err != nil {
			return fmt.Errorf("jwks: chave %q: %w", item.Kid, err)
		}
		keys[item.Kid] = pub
	}
	if len(keys) == 0 {
		return errors.New("jwks: nenhuma chave RSA utilizável")
	}

	v.keys = keys
	v.fetchedAt = time.Now()
	return nil
}

func parseRSAPublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("exponent: %w", err)
	}
	if len(eBytes) == 0 {
		return nil, errors.New("exponent vazio")
	}

	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

// Warmup busca o JWKS uma vez no start, falhando cedo se o IdP estiver inacessível.
func (v *JWKSValidator) Warmup(ctx context.Context) error {
	return v.refreshKeys(ctx)
}
