//go:build integration

package integration

import (
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/platform/postgres"
)

// A reversão precisa ser executável, não apenas documentada: um `down` que não
// roda é um plano de rollback que só falha na hora errada.
func TestMigrationsApplyAndRevert(t *testing.T) {
	truncateAll(t)

	migrator, err := postgres.NewMigrator(databaseDSN)
	if err != nil {
		t.Fatalf("não foi possível montar o migrador: %v", err)
	}
	t.Cleanup(func() {
		if err := migrator.Close(); err != nil {
			t.Errorf("falha ao encerrar o migrador: %v", err)
		}
	})

	applied, dirty, err := migrator.Version()
	if err != nil {
		t.Fatalf("não foi possível consultar a versão: %v", err)
	}
	if dirty {
		t.Fatal("o schema não deveria estar sujo")
	}
	if applied == 0 {
		t.Fatal("esperava pelo menos uma migration aplicada")
	}

	if err := migrator.Down(0); err != nil {
		t.Fatalf("reversão falhou: %v", err)
	}

	for _, table := range []string{
		"wallets", "wager_transactions", "wallet_ledger_entries", "inbox_messages", "outbox_events",
	} {
		if tableExists(t, table) {
			t.Errorf("a tabela %s deveria ter sido removida pela reversão", table)
		}
	}

	// O gatilho de imutabilidade também precisa sair, senão uma reaplicação
	// falharia por objeto já existente.
	if functionExists(t, "wallet_ledger_entries_reject_mutation") {
		t.Error("a função do gatilho deveria ter sido removida")
	}

	if err := migrator.Up(); err != nil {
		t.Fatalf("reaplicação falhou: %v", err)
	}

	reapplied, dirty, err := migrator.Version()
	if err != nil {
		t.Fatalf("não foi possível consultar a versão: %v", err)
	}
	if dirty {
		t.Fatal("o schema não deveria estar sujo após a reaplicação")
	}
	if reapplied != applied {
		t.Errorf("versão após reaplicar = %d, esperada %d", reapplied, applied)
	}

	for _, table := range []string{
		"wallets", "wager_transactions", "wallet_ledger_entries", "inbox_messages", "outbox_events",
	} {
		if !tableExists(t, table) {
			t.Errorf("a tabela %s deveria existir após a reaplicação", table)
		}
	}
}

// Aplicar de novo com tudo em dia não é erro: o comando é idempotente.
func TestMigrationsUpIsIdempotent(t *testing.T) {
	migrator, err := postgres.NewMigrator(databaseDSN)
	if err != nil {
		t.Fatalf("não foi possível montar o migrador: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })

	if err := migrator.Up(); err != nil {
		t.Fatalf("aplicar sem pendências não deveria falhar: %v", err)
	}
}

func TestMigratorRejectsInvalidDSN(t *testing.T) {
	if _, err := postgres.NewMigrator("isto-não-é-um-dsn"); err == nil {
		t.Fatal("esperava erro para DSN inválido")
	}
}

func tableExists(t *testing.T, name string) bool {
	t.Helper()

	var exists bool
	err := pool.QueryRow(testContext(t), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("não foi possível consultar a tabela %s: %v", name, err)
	}
	return exists
}

func functionExists(t *testing.T, name string) bool {
	t.Helper()

	var exists bool
	err := pool.QueryRow(testContext(t), `
		SELECT EXISTS (
			SELECT 1 FROM pg_proc p
			JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'public' AND p.proname = $1
		)`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("não foi possível consultar a função %s: %v", name, err)
	}
	return exists
}
