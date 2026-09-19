// Package config carrega e valida a configuração da aplicação a partir do ambiente.
//
// A validação é fail-fast e agrega todos os problemas encontrados em um único
// erro, para que uma instância mal configurada não suba pela metade.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Ambientes suportados.
const (
	EnvLocal      = "local"
	EnvTest       = "test"
	EnvProduction = "production"
)

// Lookup resolve uma variável de ambiente. Permite testar o carregamento sem
// tocar no ambiente do processo.
type Lookup func(key string) (string, bool)

// Config agrega toda a configuração da aplicação.
type Config struct {
	Env      string
	Log      Log
	HTTP     HTTP
	Postgres Postgres
	SQS      SQS
	OIDC     OIDC
}

// Log controla a saída estruturada.
type Log struct {
	Level string
}

// HTTP controla o servidor da API.
type HTTP struct {
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

// Postgres controla o pool de conexões.
type Postgres struct {
	DSN            string
	MaxConns       int32
	MinConns       int32
	ConnectTimeout time.Duration
}

// SQS controla o acesso às filas.
type SQS struct {
	Region              string
	Endpoint            string
	AccessKeyID         string
	SecretAccessKey     string
	WagerQueueURL       string
	WagerDLQURL         string
	IntegrationQueueURL string
}

// OIDC controla a validação de tokens emitidos pelo IdP externo.
type OIDC struct {
	IssuerURL   string
	Audience    string
	JWKSURL     string
	JWKSRefresh time.Duration
}

// Error reúne todos os problemas de configuração encontrados.
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return "configuração inválida: " + strings.Join(e.Problems, "; ")
}

// Load carrega a configuração a partir do ambiente do processo.
func Load() (Config, error) {
	return LoadFrom(os.LookupEnv)
}

// LoadFrom carrega a configuração a partir de um resolvedor arbitrário.
func LoadFrom(lookup Lookup) (Config, error) {
	r := &reader{lookup: lookup}

	cfg := Config{
		Env: r.enum("APP_ENV", EnvLocal, EnvLocal, EnvTest, EnvProduction),
		Log: Log{
			Level: r.enum("LOG_LEVEL", "info", "debug", "info", "warn", "error"),
		},
		HTTP: HTTP{
			Port:            r.integer("HTTP_PORT", 8080, 0, 65535),
			ReadTimeout:     r.duration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    r.duration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:     r.duration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: r.duration("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
		},
		Postgres: Postgres{
			DSN:            r.required("POSTGRES_DSN"),
			MaxConns:       int32(r.integer("POSTGRES_MAX_CONNS", 10, 1, 1000)),
			MinConns:       int32(r.integer("POSTGRES_MIN_CONNS", 0, 0, 1000)),
			ConnectTimeout: r.duration("POSTGRES_CONNECT_TIMEOUT", 5*time.Second),
		},
		SQS: SQS{
			Region:              r.optional("AWS_REGION", "us-east-1"),
			Endpoint:            r.optional("AWS_ENDPOINT_URL", ""),
			AccessKeyID:         r.optional("AWS_ACCESS_KEY_ID", ""),
			SecretAccessKey:     r.optional("AWS_SECRET_ACCESS_KEY", ""),
			WagerQueueURL:       r.required("SQS_WAGER_QUEUE_URL"),
			WagerDLQURL:         r.required("SQS_WAGER_DLQ_URL"),
			IntegrationQueueURL: r.required("SQS_INTEGRATION_QUEUE_URL"),
		},
		OIDC: OIDC{
			IssuerURL:   r.required("OIDC_ISSUER_URL"),
			Audience:    r.required("OIDC_AUDIENCE"),
			JWKSURL:     r.optional("OIDC_JWKS_URL", ""),
			JWKSRefresh: r.duration("OIDC_JWKS_REFRESH", 5*time.Minute),
		},
	}

	// O `iss` do token reflete o hostname externo do Keycloak, que pode não ser
	// resolvível de dentro da rede do Compose. Quando não há override, o JWKS é
	// buscado no próprio issuer.
	if cfg.OIDC.JWKSURL == "" && cfg.OIDC.IssuerURL != "" {
		cfg.OIDC.JWKSURL = strings.TrimRight(cfg.OIDC.IssuerURL, "/") + "/protocol/openid-connect/certs"
	}

	if cfg.Postgres.MinConns > cfg.Postgres.MaxConns {
		r.problem("POSTGRES_MIN_CONNS (%d) não pode ser maior que POSTGRES_MAX_CONNS (%d)",
			cfg.Postgres.MinConns, cfg.Postgres.MaxConns)
	}

	if len(r.problems) > 0 {
		return Config{}, &Error{Problems: r.problems}
	}
	return cfg, nil
}

type reader struct {
	lookup   Lookup
	problems []string
}

// value devolve o valor sem espaços em branco; vazio equivale a ausente.
func (r *reader) value(key string) (string, bool) {
	raw, ok := r.lookup(key)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func (r *reader) required(key string) string {
	v, ok := r.value(key)
	if !ok {
		r.problem("%s é obrigatório", key)
		return ""
	}
	return v
}

func (r *reader) optional(key, fallback string) string {
	if v, ok := r.value(key); ok {
		return v
	}
	return fallback
}

func (r *reader) enum(key, fallback string, allowed ...string) string {
	v := r.optional(key, fallback)
	for _, candidate := range allowed {
		if v == candidate {
			return v
		}
	}
	r.problem("%s deve ser um de [%s], recebido %q", key, strings.Join(allowed, ", "), v)
	return fallback
}

func (r *reader) integer(key string, fallback, min, max int) int {
	raw, ok := r.value(key)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		r.problem("%s deve ser um inteiro, recebido %q", key, raw)
		return fallback
	}
	if n < min || n > max {
		r.problem("%s deve estar entre %d e %d, recebido %d", key, min, max, n)
		return fallback
	}
	return n
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	raw, ok := r.value(key)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		r.problem("%s deve ser uma duração válida (ex.: 5s), recebido %q", key, raw)
		return fallback
	}
	if d <= 0 {
		r.problem("%s deve ser maior que zero, recebido %q", key, raw)
		return fallback
	}
	return d
}

func (r *reader) problem(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}
