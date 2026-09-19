//go:build integration

// Package integration exercita o schema contra um PostgreSQL real.
//
// O desafio proíbe substituir a infraestrutura por mocks: uma constraint só
// está provada quando o próprio banco recusa a violação.
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	platformpostgres "github.com/vigmi/backend-challenge-go/internal/platform/postgres"
)

var (
	// databaseDSN aponta para o PostgreSQL levantado em TestMain.
	databaseDSN string
	// pool é compartilhado pelos testes; cada teste limpa o que criou.
	pool *pgxpool.Pool
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("wagering"),
		postgres.WithUsername("wagering"),
		postgres.WithPassword("wagering"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "não foi possível subir o PostgreSQL: %v\n", err)
		os.Exit(1)
	}

	code := run(ctx, container, m)

	if err := testcontainers.TerminateContainer(container); err != nil {
		fmt.Fprintf(os.Stderr, "falha ao encerrar o container: %v\n", err)
	}
	os.Exit(code)
}

func run(ctx context.Context, container *postgres.PostgresContainer, m *testing.M) int {
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "não foi possível obter o DSN: %v\n", err)
		return 1
	}
	databaseDSN = dsn

	if err := applyMigrations(dsn); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "não foi possível abrir o pool: %v\n", err)
		return 1
	}
	defer pool.Close()

	return m.Run()
}

func applyMigrations(dsn string) error {
	migrator, err := platformpostgres.NewMigrator(dsn)
	if err != nil {
		return fmt.Errorf("não foi possível montar o migrador: %w", err)
	}
	defer func() { _ = migrator.Close() }()

	if err := migrator.Up(); err != nil {
		return fmt.Errorf("não foi possível aplicar as migrations: %w", err)
	}
	return nil
}

// truncateAll devolve o banco ao estado vazio entre testes.
func truncateAll(t *testing.T) {
	t.Helper()

	_, err := pool.Exec(context.Background(), `
		TRUNCATE outbox_events, inbox_messages, wallet_ledger_entries, wager_transactions, wallets
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("não foi possível limpar as tabelas: %v", err)
	}
}
