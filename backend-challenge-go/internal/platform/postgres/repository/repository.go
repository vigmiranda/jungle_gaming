// Package repository implementa os ports de persistência com `pgx` e SQL
// explícito.
//
// Nada de ORM: transações, locks e constraints ficam visíveis no código, que é
// o que permite auditar onde a unidade de trabalho começa e termina.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
)

// querier abstrai pool e transação: os repositórios funcionam nos dois modos,
// mas só a transação garante atomicidade entre eles.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Códigos SQLSTATE relevantes.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
)

// translate converte a falha do PostgreSQL em erro de aplicação.
//
// O nome da constraint entra na mensagem: quando uma invariante do banco é
// violada, saber qual foi encurta o diagnóstico.
func translate(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return port.ErrNotFound.Messagef("%s: registro não encontrado", operation)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case codeUniqueViolation:
			return port.ErrConflict.Messagef("%s: violação de unicidade (%s)", operation, pgErr.ConstraintName).
				WithCause(err)
		case codeForeignKeyViolation, codeCheckViolation:
			return port.ErrConflict.Messagef("%s: invariante do banco violada (%s)", operation, pgErr.ConstraintName).
				WithCause(err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// requireSingleRow confirma que a escrita atingiu exatamente uma linha.
//
// Um UPDATE que não encontra a linha é falha de premissa, não sucesso silencioso.
func requireSingleRow(operation string, tag pgconn.CommandTag) error {
	if tag.RowsAffected() != 1 {
		return port.ErrNotFound.Messagef("%s: %d linhas afetadas, esperada 1", operation, tag.RowsAffected())
	}
	return nil
}

// repositories agrupa os repositórios de uma mesma transação.
type repositories struct {
	wallets      *WalletRepository
	transactions *TransactionRepository
	ledger       *LedgerRepository
}

func newRepositories(db querier) *repositories {
	return &repositories{
		wallets:      &WalletRepository{db: db},
		transactions: &TransactionRepository{db: db},
		ledger:       &LedgerRepository{db: db},
	}
}

func (r *repositories) Wallets() port.WalletRepository           { return r.wallets }
func (r *repositories) Transactions() port.TransactionRepository { return r.transactions }
func (r *repositories) Ledger() port.LedgerRepository            { return r.ledger }
