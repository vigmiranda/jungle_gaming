package postgres

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxdriver "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // driver database/sql usado pelas migrations

	"github.com/vigmi/backend-challenge-go/migrations"
)

// Migrator aplica e reverte as migrations versionadas.
//
// As migrations são executadas por um comando próprio, nunca no start da
// aplicação: com várias instâncias subindo ao mesmo tempo, migrar no boot
// transforma o deploy em uma corrida.
type Migrator struct {
	migrate *migrate.Migrate
}

// NewMigrator abre a conexão de migração a partir do DSN.
func NewMigrator(dsn string) (*Migrator, error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("migrations: não foi possível ler os arquivos embarcados: %w", err)
	}

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("migrations: DSN inválido: %w", err)
	}

	driver, err := pgxdriver.WithInstance(database, &pgxdriver.Config{})
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrations: não foi possível conectar ao PostgreSQL: %w", err)
	}

	instance, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrations: não foi possível montar o migrador: %w", err)
	}

	return &Migrator{migrate: instance}, nil
}

// Up aplica todas as migrations pendentes. Sem pendências, não é erro.
func (m *Migrator) Up() error {
	if err := m.migrate.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: falha ao aplicar: %w", err)
	}
	return nil
}

// Down reverte as migrations aplicadas. Com `steps` igual a zero, reverte tudo.
func (m *Migrator) Down(steps int) error {
	var err error
	if steps <= 0 {
		err = m.migrate.Down()
	} else {
		err = m.migrate.Steps(-steps)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrations: falha ao reverter: %w", err)
	}
	return nil
}

// Version devolve a versão aplicada e se o schema está sujo.
//
// O estado sujo significa que uma migration falhou no meio e exige intervenção
// manual: seguir aplicando por cima esconderia um schema parcial.
func (m *Migrator) Version() (version uint, dirty bool, err error) {
	version, dirty, err = m.migrate.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("migrations: falha ao consultar a versão: %w", err)
	}
	return version, dirty, nil
}

// Close libera a conexão de migração.
func (m *Migrator) Close() error {
	sourceErr, databaseErr := m.migrate.Close()
	return errors.Join(sourceErr, databaseErr)
}
