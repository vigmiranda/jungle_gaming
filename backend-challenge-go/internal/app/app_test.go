package app_test

import (
	"testing"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/vigmi/backend-challenge-go/internal/app"
	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/httpserver"
)

// testConfig usa porta 0 e um DSN sintaticamente válido: o start não deve
// depender de infraestrutura no ar, apenas da composição estar correta.
func testConfig() config.Config {
	return config.Config{
		Env: config.EnvTest,
		Log: config.Log{Level: "error"},
		HTTP: config.HTTP{
			Port:            0,
			ReadTimeout:     time.Second,
			WriteTimeout:    time.Second,
			IdleTimeout:     time.Second,
			ShutdownTimeout: 2 * time.Second,
		},
		Postgres: config.Postgres{
			DSN:            "postgres://wagering:wagering@127.0.0.1:5432/wagering?sslmode=disable",
			MaxConns:       2,
			MinConns:       0,
			ConnectTimeout: time.Second,
		},
		SQS: config.SQS{
			Region:              "us-east-1",
			Endpoint:            "http://127.0.0.1:4566",
			AccessKeyID:         "test",
			SecretAccessKey:     "test",
			WagerQueueURL:       "http://127.0.0.1:4566/000000000000/wager-transactions.fifo",
			WagerDLQURL:         "http://127.0.0.1:4566/000000000000/wager-transactions-dlq.fifo",
			IntegrationQueueURL: "http://127.0.0.1:4566/000000000000/wagering-integration-events",
		},
		OIDC: config.OIDC{
			IssuerURL:   "http://127.0.0.1:8088/realms/wagering",
			Audience:    "wagering-api",
			JWKSRefresh: time.Minute,
		},
	}
}

func TestModuleIsValid(t *testing.T) {
	if err := fx.ValidateApp(app.Module(), fx.Replace(testConfig())); err != nil {
		t.Fatalf("grafo de dependências inválido: %v", err)
	}
}

func TestAppStartsAndStopsReleasingResources(t *testing.T) {
	var server *httpserver.Server

	fxApp := fxtest.New(t,
		app.Module(),
		fx.Replace(testConfig()),
		fx.Populate(&server),
	)

	fxApp.RequireStart()

	if server.Addr() == "" {
		t.Error("servidor HTTP deveria estar escutando após o start")
	}

	fxApp.RequireStop()

	if err := fxApp.Err(); err != nil {
		t.Fatalf("aplicação reportou erro no ciclo de vida: %v", err)
	}
}

func TestAppFailsFastOnInvalidConfiguration(t *testing.T) {
	invalid := testConfig()
	invalid.Postgres.DSN = "isto-não-é-um-dsn"

	fxApp := fx.New(
		app.Module(),
		fx.Replace(invalid),
		fx.NopLogger,
	)

	if err := fxApp.Err(); err == nil {
		t.Fatal("esperava falha na construção do grafo com DSN inválido")
	}
}
