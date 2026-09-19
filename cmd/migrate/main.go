// Command migrate aplica e reverte as migrations versionadas.
//
// As migrations rodam por este comando, e não no start da aplicação: com
// várias instâncias subindo em paralelo, migrar no boot vira uma corrida.
//
//	migrate -command up
//	migrate -command down -steps 1
//	migrate -command down -steps 0   # reverte tudo
//	migrate -command version
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/vigmi/backend-challenge-go/internal/platform/postgres"
)

func main() {
	command := flag.String("command", "up", "up, down ou version")
	steps := flag.Int("steps", 1, "quantidade de migrations a reverter; 0 reverte todas")
	dsn := flag.String("dsn", os.Getenv("POSTGRES_DSN"), "DSN do PostgreSQL; por padrão usa POSTGRES_DSN")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("migrate: informe o DSN em -dsn ou na variável POSTGRES_DSN")
	}

	migrator, err := postgres.NewMigrator(*dsn)
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}
	defer func() {
		if err := migrator.Close(); err != nil {
			log.Printf("migrate: falha ao encerrar: %v", err)
		}
	}()

	if err := run(migrator, *command, *steps); err != nil {
		log.Fatalf("migrate: %v", err)
	}
}

func run(migrator *postgres.Migrator, command string, steps int) error {
	switch command {
	case "up":
		if err := migrator.Up(); err != nil {
			return err
		}
		return report(migrator, "migrations aplicadas")

	case "down":
		if err := migrator.Down(steps); err != nil {
			return err
		}
		return report(migrator, "migrations revertidas")

	case "version":
		return report(migrator, "versão atual")

	default:
		return fmt.Errorf("comando %q não é suportado; use up, down ou version", command)
	}
}

func report(migrator *postgres.Migrator, prefix string) error {
	version, dirty, err := migrator.Version()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("schema sujo na versão %d: uma migration falhou no meio e exige intervenção manual", version)
	}
	log.Printf("%s; versão %d", prefix, version)
	return nil
}
