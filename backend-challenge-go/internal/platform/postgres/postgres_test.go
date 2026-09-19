package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx/fxtest"

	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/postgres"
)

func poolConfig(dsn string) config.Config {
	return config.Config{Postgres: config.Postgres{
		DSN:            dsn,
		MaxConns:       2,
		MinConns:       0,
		ConnectTimeout: time.Second,
	}}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestNewPoolRejectsInvalidDSN(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)

	_, err := postgres.NewPool(lifecycle, poolConfig("isto-não-é-um-dsn"), discardLogger())

	if err == nil {
		t.Fatal("esperava erro para DSN inválido")
	}
	if !strings.Contains(err.Error(), "DSN inválido") {
		t.Errorf("mensagem inesperada: %v", err)
	}
}

func TestNewPoolRegistersShutdownHook(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)

	pool, err := postgres.NewPool(lifecycle,
		poolConfig("postgres://wagering:wagering@127.0.0.1:5432/wagering?sslmode=disable"),
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("NewPool devolveu erro: %v", err)
	}

	lifecycle.RequireStart()
	lifecycle.RequireStop()

	// O pool fechado recusa novas aquisições, provando que o hook rodou.
	if _, err := pool.Acquire(context.Background()); err == nil {
		t.Error("esperava erro ao usar o pool após o stop")
	}
}

func TestProbeIsNamedPostgres(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	pool, err := postgres.NewPool(lifecycle,
		poolConfig("postgres://wagering:wagering@127.0.0.1:5432/wagering?sslmode=disable"),
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("NewPool devolveu erro: %v", err)
	}
	defer pool.Close()

	if name := postgres.NewProbe(pool).Name(); name != "postgres" {
		t.Errorf("Name = %q, esperado \"postgres\"", name)
	}
}
