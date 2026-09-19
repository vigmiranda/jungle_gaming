// Package port declara as dependências de saída da camada de aplicação.
//
// As interfaces vivem aqui, do lado de quem consome, e não junto da
// implementação: o domínio e os casos de uso não conhecem `pgx`, e os adapters
// é que se adaptam ao contrato.
package port

import (
	"context"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// Repositories reúne os repositórios de uma mesma unidade de trabalho.
//
// Todos compartilham a transação SQL aberta pelo `UnitOfWork`, o que garante
// que estado da operação, saldo, ledger e eventos sejam confirmados juntos.
type Repositories interface {
	Wallets() WalletRepository
	Transactions() TransactionRepository
	Ledger() LedgerRepository
}

// UnitOfWork delimita a transação SQL de uma operação financeira.
//
// O escopo é explícito: a transação começa ao entrar em `Execute` e termina no
// commit, quando a função devolve nil, ou no rollback, quando devolve erro.
// Nenhum repositório abre commit por conta própria.
type UnitOfWork interface {
	Execute(ctx context.Context, fn func(ctx context.Context, repositories Repositories) error) error

	// ExecuteReadOnly roda a função em uma transação somente leitura com
	// isolamento `REPEATABLE READ`.
	//
	// A reconciliação precisa comparar saldo e ledger na mesma visão dos dados:
	// sob `READ COMMITTED`, duas consultas da mesma transação enxergariam
	// instantes diferentes e reportariam divergência onde não há.
	ExecuteReadOnly(ctx context.Context, fn func(ctx context.Context, repositories Repositories) error) error
}

// WalletRepository persiste o agregado de carteira.
type WalletRepository interface {
	// Create insere a carteira. Devolve ErrConflict quando já existe carteira
	// para o par (jogador, moeda).
	Create(ctx context.Context, target *wallet.Wallet) error

	// LockByID carrega a carteira com `SELECT ... FOR UPDATE`, serializando os
	// escritores daquela linha até o fim da transação. Escritores de carteiras
	// diferentes não se bloqueiam.
	LockByID(ctx context.Context, id shared.ID) (*wallet.Wallet, error)

	// FindByID lê a carteira sem lock, para consultas que não movimentam saldo.
	FindByID(ctx context.Context, id shared.ID) (*wallet.Wallet, error)

	// UpdateBalance grava saldo, versão e instante de atualização.
	UpdateBalance(ctx context.Context, target *wallet.Wallet) error
}

// TransactionRepository persiste a operação e sustenta a idempotência.
type TransactionRepository interface {
	Create(ctx context.Context, target *wagering.Transaction) error
	Update(ctx context.Context, target *wagering.Transaction) error
	FindByID(ctx context.Context, id shared.ID) (*wagering.Transaction, error)

	// FindByIdempotencyKey e FindByExternalID resolvem replay e conflito. O
	// escopo é por provedor: a chave pertence ao cliente e não cruza tenants.
	FindByIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (*wagering.Transaction, error)
	FindByExternalID(ctx context.Context, providerID, externalID string) (*wagering.Transaction, error)

	// HasSuccessfulReversal indica se a referência já recebeu qualquer reversão
	// bem-sucedida, de qualquer tipo.
	//
	// A checagem não é por tipo: um `REFUND` seguido de um `ROLLBACK` da mesma
	// aposta devolveria o mesmo débito duas vezes. A unicidade por tipo no
	// schema é a rede de proteção; a coerência financeira é decidida aqui, sob
	// o lock da carteira, o que também impede a corrida entre duas reversões.
	HasSuccessfulReversal(ctx context.Context, referenceID shared.ID) (bool, error)
}

// LedgerCursor localiza a página seguinte do extrato.
//
// A ordenação é por `(created_at, id)`, estável mesmo com lançamentos no mesmo
// instante. A codificação opaca é responsabilidade da borda HTTP.
type LedgerCursor struct {
	CreatedAt time.Time
	ID        shared.ID
}

// LedgerPage é uma página do extrato.
type LedgerPage struct {
	Entries []ledger.Entry
	// Next é nil quando não há mais páginas.
	Next *LedgerCursor
}

// LedgerRepository registra e lê os lançamentos.
type LedgerRepository interface {
	// Append grava o lançamento. Devolve ErrConflict quando a transação já
	// produziu um lançamento naquela carteira.
	Append(ctx context.Context, entry ledger.Entry) error

	// ListByWallet devolve uma página ordenada de forma estável.
	ListByWallet(ctx context.Context, walletID shared.ID, after *LedgerCursor, limit int) (LedgerPage, error)

	// SumByWallet reconstrói o saldo somando créditos e subtraindo débitos,
	// base da reconciliação.
	SumByWallet(ctx context.Context, walletID shared.ID) (money.Money, int, error)
}
