package config_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/config"
)

func lookupFrom(env map[string]string) config.Lookup {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

// minimalEnv contém apenas as variáveis obrigatórias.
func minimalEnv() map[string]string {
	return map[string]string{
		"POSTGRES_DSN":              "postgres://user:pass@localhost:5432/db",
		"SQS_WAGER_QUEUE_URL":       "http://localhost:4566/000000000000/wager-transactions.fifo",
		"SQS_WAGER_DLQ_URL":         "http://localhost:4566/000000000000/wager-transactions-dlq.fifo",
		"SQS_INTEGRATION_QUEUE_URL": "http://localhost:4566/000000000000/wagering-integration-events",
		"OIDC_ISSUER_URL":           "http://localhost:8088/realms/wagering",
		"OIDC_AUDIENCE":             "wagering-api",
	}
}

func TestLoadFromAppliesDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(lookupFrom(minimalEnv()))
	if err != nil {
		t.Fatalf("esperava configuração válida, obteve erro: %v", err)
	}

	if cfg.Env != config.EnvLocal {
		t.Errorf("Env = %q, esperado %q", cfg.Env, config.EnvLocal)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, esperado \"info\"", cfg.Log.Level)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("HTTP.Port = %d, esperado 8080", cfg.HTTP.Port)
	}
	if cfg.HTTP.ReadTimeout != 15*time.Second {
		t.Errorf("HTTP.ReadTimeout = %s, esperado 15s", cfg.HTTP.ReadTimeout)
	}
	if cfg.HTTP.WriteTimeout != 15*time.Second {
		t.Errorf("HTTP.WriteTimeout = %s, esperado 15s", cfg.HTTP.WriteTimeout)
	}
	if cfg.HTTP.IdleTimeout != 60*time.Second {
		t.Errorf("HTTP.IdleTimeout = %s, esperado 60s", cfg.HTTP.IdleTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != 20*time.Second {
		t.Errorf("HTTP.ShutdownTimeout = %s, esperado 20s", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.Postgres.MaxConns != 10 || cfg.Postgres.MinConns != 0 {
		t.Errorf("pool = (%d, %d), esperado (10, 0)", cfg.Postgres.MinConns, cfg.Postgres.MaxConns)
	}
	if cfg.Postgres.ConnectTimeout != 5*time.Second {
		t.Errorf("Postgres.ConnectTimeout = %s, esperado 5s", cfg.Postgres.ConnectTimeout)
	}
	if cfg.SQS.Region != "us-east-1" {
		t.Errorf("SQS.Region = %q, esperado \"us-east-1\"", cfg.SQS.Region)
	}
	if cfg.SQS.Endpoint != "" || cfg.SQS.AccessKeyID != "" || cfg.SQS.SecretAccessKey != "" {
		t.Errorf("credenciais AWS deveriam ser opcionais e vazias por padrão: %+v", cfg.SQS)
	}
	if !cfg.SQS.PublisherEnabled {
		t.Error("PublisherEnabled deveria ser true por padrão")
	}
	if cfg.SQS.PublisherBatchSize != 10 || cfg.SQS.PublisherLeaseTTL != 30*time.Second {
		t.Errorf("publisher defaults incorretos: %+v", cfg.SQS)
	}
	if cfg.OIDC.JWKSRefresh != 5*time.Minute {
		t.Errorf("OIDC.JWKSRefresh = %s, esperado 5m", cfg.OIDC.JWKSRefresh)
	}
	if want := "http://localhost:8088/realms/wagering/protocol/openid-connect/certs"; cfg.OIDC.JWKSURL != want {
		t.Errorf("OIDC.JWKSURL = %q, esperado derivar do issuer (%q)", cfg.OIDC.JWKSURL, want)
	}
}

func TestLoadFromDerivesJWKSURLIgnoringTrailingSlash(t *testing.T) {
	env := minimalEnv()
	env["OIDC_ISSUER_URL"] = "http://localhost:8088/realms/wagering/"

	cfg, err := config.LoadFrom(lookupFrom(env))
	if err != nil {
		t.Fatalf("esperava configuração válida, obteve erro: %v", err)
	}
	if want := "http://localhost:8088/realms/wagering/protocol/openid-connect/certs"; cfg.OIDC.JWKSURL != want {
		t.Errorf("OIDC.JWKSURL = %q, esperado %q", cfg.OIDC.JWKSURL, want)
	}
}

// O issuer do token reflete o hostname externo do Keycloak; dentro da rede do
// Compose o JWKS precisa ser buscado pelo nome interno do serviço.
func TestLoadFromKeepsExplicitJWKSURL(t *testing.T) {
	env := minimalEnv()
	env["OIDC_JWKS_URL"] = "http://keycloak:8088/realms/wagering/protocol/openid-connect/certs"

	cfg, err := config.LoadFrom(lookupFrom(env))
	if err != nil {
		t.Fatalf("esperava configuração válida, obteve erro: %v", err)
	}
	if cfg.OIDC.JWKSURL != "http://keycloak:8088/realms/wagering/protocol/openid-connect/certs" {
		t.Errorf("OIDC.JWKSURL = %q, o override deveria prevalecer", cfg.OIDC.JWKSURL)
	}
	if cfg.OIDC.IssuerURL != "http://localhost:8088/realms/wagering" {
		t.Errorf("OIDC.IssuerURL = %q, não deveria mudar", cfg.OIDC.IssuerURL)
	}
}

func TestLoadFromLeavesJWKSURLEmptyWhenIssuerMissing(t *testing.T) {
	_, err := config.LoadFrom(lookupFrom(map[string]string{}))
	if err == nil {
		t.Fatal("esperava erro de configuração sem issuer")
	}
}

func TestLoadFromReadsExplicitValues(t *testing.T) {
	env := minimalEnv()
	env["APP_ENV"] = config.EnvProduction
	env["LOG_LEVEL"] = "debug"
	env["HTTP_PORT"] = "9090"
	env["HTTP_READ_TIMEOUT"] = "1s"
	env["HTTP_WRITE_TIMEOUT"] = "2s"
	env["HTTP_IDLE_TIMEOUT"] = "3s"
	env["HTTP_SHUTDOWN_TIMEOUT"] = "4s"
	env["POSTGRES_MAX_CONNS"] = "42"
	env["POSTGRES_MIN_CONNS"] = "7"
	env["POSTGRES_CONNECT_TIMEOUT"] = "9s"
	env["AWS_REGION"] = "sa-east-1"
	env["AWS_ENDPOINT_URL"] = "http://localstack:4566"
	env["AWS_ACCESS_KEY_ID"] = "test"
	env["AWS_SECRET_ACCESS_KEY"] = "secret"
	env["OIDC_JWKS_REFRESH"] = "30s"

	cfg, err := config.LoadFrom(lookupFrom(env))
	if err != nil {
		t.Fatalf("esperava configuração válida, obteve erro: %v", err)
	}

	if cfg.Env != config.EnvProduction || cfg.Log.Level != "debug" {
		t.Errorf("Env/Log = (%q, %q)", cfg.Env, cfg.Log.Level)
	}
	if cfg.HTTP.Port != 9090 {
		t.Errorf("HTTP.Port = %d, esperado 9090", cfg.HTTP.Port)
	}
	if cfg.HTTP.ReadTimeout != time.Second || cfg.HTTP.WriteTimeout != 2*time.Second ||
		cfg.HTTP.IdleTimeout != 3*time.Second || cfg.HTTP.ShutdownTimeout != 4*time.Second {
		t.Errorf("timeouts HTTP incorretos: %+v", cfg.HTTP)
	}
	if cfg.Postgres.MaxConns != 42 || cfg.Postgres.MinConns != 7 ||
		cfg.Postgres.ConnectTimeout != 9*time.Second {
		t.Errorf("configuração de Postgres incorreta: %+v", cfg.Postgres)
	}
	if cfg.SQS.Region != "sa-east-1" || cfg.SQS.Endpoint != "http://localstack:4566" ||
		cfg.SQS.AccessKeyID != "test" || cfg.SQS.SecretAccessKey != "secret" {
		t.Errorf("configuração de SQS incorreta: %+v", cfg.SQS)
	}
	if cfg.OIDC.JWKSRefresh != 30*time.Second {
		t.Errorf("OIDC.JWKSRefresh = %s, esperado 30s", cfg.OIDC.JWKSRefresh)
	}
}

func TestLoadFromTreatsBlankAsAbsent(t *testing.T) {
	env := minimalEnv()
	env["HTTP_PORT"] = "   "
	env["AWS_REGION"] = ""
	env["LOG_LEVEL"] = " warn "

	cfg, err := config.LoadFrom(lookupFrom(env))
	if err != nil {
		t.Fatalf("esperava configuração válida, obteve erro: %v", err)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("HTTP.Port = %d, esperado o padrão 8080", cfg.HTTP.Port)
	}
	if cfg.SQS.Region != "us-east-1" {
		t.Errorf("SQS.Region = %q, esperado o padrão", cfg.SQS.Region)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("Log.Level = %q, esperado \"warn\" após trim", cfg.Log.Level)
	}
}

func TestLoadFromReportsMissingRequired(t *testing.T) {
	_, err := config.LoadFrom(lookupFrom(map[string]string{}))
	if err == nil {
		t.Fatal("esperava erro de configuração")
	}

	var cfgErr *config.Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("esperava *config.Error, obteve %T", err)
	}

	required := []string{
		"POSTGRES_DSN",
		"SQS_WAGER_QUEUE_URL",
		"SQS_WAGER_DLQ_URL",
		"SQS_INTEGRATION_QUEUE_URL",
		"OIDC_ISSUER_URL",
		"OIDC_AUDIENCE",
	}
	if len(cfgErr.Problems) != len(required) {
		t.Fatalf("esperava %d problemas, obteve %d: %v", len(required), len(cfgErr.Problems), cfgErr.Problems)
	}
	for _, key := range required {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("erro não menciona %s: %s", key, err.Error())
		}
	}
}

func TestLoadFromRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantMsg string
	}{
		{"ambiente desconhecido", "APP_ENV", "staging", "APP_ENV deve ser um de"},
		{"nível de log desconhecido", "LOG_LEVEL", "verbose", "LOG_LEVEL deve ser um de"},
		{"porta não numérica", "HTTP_PORT", "oito mil", "HTTP_PORT deve ser um inteiro"},
		{"porta fora do intervalo", "HTTP_PORT", "70000", "HTTP_PORT deve estar entre 0 e 65535"},
		{"porta negativa", "HTTP_PORT", "-1", "HTTP_PORT deve estar entre 0 e 65535"},
		{"conexões fora do intervalo", "POSTGRES_MAX_CONNS", "0", "POSTGRES_MAX_CONNS deve estar entre 1 e 1000"},
		{"duração inválida", "HTTP_READ_TIMEOUT", "5 segundos", "HTTP_READ_TIMEOUT deve ser uma duração válida"},
		{"duração zero", "HTTP_READ_TIMEOUT", "0s", "HTTP_READ_TIMEOUT deve ser maior que zero"},
		{"duração negativa", "OIDC_JWKS_REFRESH", "-1m", "OIDC_JWKS_REFRESH deve ser maior que zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := minimalEnv()
			env[tt.key] = tt.value

			_, err := config.LoadFrom(lookupFrom(env))
			if err == nil {
				t.Fatalf("esperava erro para %s=%q", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("erro = %q, esperava conter %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestLoadFromRejectsMinConnsAboveMaxConns(t *testing.T) {
	env := minimalEnv()
	env["POSTGRES_MAX_CONNS"] = "5"
	env["POSTGRES_MIN_CONNS"] = "6"

	_, err := config.LoadFrom(lookupFrom(env))
	if err == nil {
		t.Fatal("esperava erro quando MIN_CONNS > MAX_CONNS")
	}
	if !strings.Contains(err.Error(), "POSTGRES_MIN_CONNS (6) não pode ser maior que POSTGRES_MAX_CONNS (5)") {
		t.Errorf("mensagem inesperada: %s", err.Error())
	}
}

func TestLoadFromAggregatesProblems(t *testing.T) {
	_, err := config.LoadFrom(lookupFrom(map[string]string{
		"APP_ENV":   "staging",
		"HTTP_PORT": "abc",
	}))
	if err == nil {
		t.Fatal("esperava erro de configuração")
	}

	var cfgErr *config.Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("esperava *config.Error, obteve %T", err)
	}
	if len(cfgErr.Problems) < 3 {
		t.Errorf("esperava múltiplos problemas agregados, obteve: %v", cfgErr.Problems)
	}
	if !strings.HasPrefix(err.Error(), "configuração inválida: ") {
		t.Errorf("prefixo inesperado: %s", err.Error())
	}
}

func TestLoadUsesProcessEnvironment(t *testing.T) {
	for key, value := range minimalEnv() {
		t.Setenv(key, value)
	}
	t.Setenv("HTTP_PORT", "8123")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("esperava configuração válida, obteve erro: %v", err)
	}
	if cfg.HTTP.Port != 8123 {
		t.Errorf("HTTP.Port = %d, esperado 8123", cfg.HTTP.Port)
	}
}
