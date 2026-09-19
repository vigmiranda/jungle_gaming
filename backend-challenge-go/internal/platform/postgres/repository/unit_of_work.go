package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
)

// UnitOfWork executa uma operação financeira dentro de uma única transação SQL.
//
// A transação começa em `Execute` e termina no commit ou no rollback: não há
// commit escondido dentro de repositório. Tudo que for efeito financeiro
// observável — estado da operação, saldo, ledger, inbox e outbox — precisa
// acontecer dentro desta função para ser confirmado junto.
type UnitOfWork struct {
	pool *pgxpool.Pool
}

// NewUnitOfWork cria a unidade de trabalho sobre o pool.
func NewUnitOfWork(pool *pgxpool.Pool) *UnitOfWork {
	return &UnitOfWork{pool: pool}
}

// Execute abre a transação, entrega os repositórios e confirma ao final.
//
// Qualquer erro devolvido pela função desfaz tudo: é o que garante que uma
// falha entre o débito e o lançamento no ledger não deixe saldo sem lastro.
func (u *UnitOfWork) Execute(
	ctx context.Context,
	fn func(ctx context.Context, repositories port.Repositories) error,
) error {
	transaction, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("unidade de trabalho: não foi possível iniciar a transação: %w", err)
	}

	// O rollback no defer cobre pânico e retorno antecipado. Depois de um commit
	// bem-sucedido ele é inofensivo: pgx devolve ErrTxClosed, que é ignorado.
	defer func() { _ = transaction.Rollback(ctx) }()

	if err := fn(ctx, newRepositories(transaction)); err != nil {
		return err
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("unidade de trabalho: falha ao confirmar a transação: %w", err)
	}
	return nil
}

// ReadOnly executa uma leitura fora da unidade de trabalho de escrita.
//
// Consultas que não movimentam saldo não precisam de transação nem de lock, e
// mantê-las fora evita segurar linhas por mais tempo que o necessário.
func (u *UnitOfWork) ReadOnly() port.Repositories {
	return newRepositories(u.pool)
}

// Garante em tempo de compilação que a transação do pgx satisfaz o querier.
var _ querier = (pgx.Tx)(nil)
