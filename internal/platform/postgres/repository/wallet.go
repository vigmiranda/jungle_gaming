package repository

import (
	"context"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// WalletRepository persiste o agregado de carteira.
type WalletRepository struct {
	db querier
}

const walletColumns = `id, player_id, currency, balance_minor, version, created_at, updated_at`

// Create insere a carteira recém-aberta.
func (r *WalletRepository) Create(ctx context.Context, target *wallet.Wallet) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wallets (`+walletColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		target.ID().String(),
		target.PlayerID().String(),
		target.Currency().String(),
		target.Balance().MinorUnits(),
		target.Version(),
		target.CreatedAt(),
		target.UpdatedAt(),
	)
	return translate("criar carteira", err)
}

// LockByID carrega a carteira segurando a linha até o fim da transação.
//
// É o ponto de serialização por carteira: dois escritores da mesma carteira se
// enfileiram aqui, enquanto carteiras diferentes seguem em paralelo. Sem lock
// global e sem retry otimista (ADR-004).
func (r *WalletRepository) LockByID(ctx context.Context, id shared.ID) (*wallet.Wallet, error) {
	return r.queryOne(ctx, "bloquear carteira", `
		SELECT `+walletColumns+`
		FROM wallets
		WHERE id = $1
		FOR UPDATE`, id.String())
}

// FindByID lê a carteira sem segurar a linha.
func (r *WalletRepository) FindByID(ctx context.Context, id shared.ID) (*wallet.Wallet, error) {
	return r.queryOne(ctx, "consultar carteira", `
		SELECT `+walletColumns+`
		FROM wallets
		WHERE id = $1`, id.String())
}

// UpdateBalance grava saldo, versão e instante de atualização.
//
// Não há predicado de versão: a linha já está bloqueada, e o `FOR UPDATE` é o
// que impede a atualização perdida. A versão é invariante de domínio e payload
// de evento, não mecanismo de concorrência.
func (r *WalletRepository) UpdateBalance(ctx context.Context, target *wallet.Wallet) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE wallets
		SET balance_minor = $2, version = $3, updated_at = $4
		WHERE id = $1`,
		target.ID().String(),
		target.Balance().MinorUnits(),
		target.Version(),
		target.UpdatedAt(),
	)
	if err != nil {
		return translate("atualizar saldo", err)
	}
	return requireSingleRow("atualizar saldo", tag)
}

func (r *WalletRepository) queryOne(ctx context.Context, operation, query string, args ...any) (*wallet.Wallet, error) {
	var (
		id           string
		playerID     string
		currencyCode string
		balanceMinor int64
		version      int64
		createdAt    time.Time
		updatedAt    time.Time
	)

	err := r.db.QueryRow(ctx, query, args...).
		Scan(&id, &playerID, &currencyCode, &balanceMinor, &version, &createdAt, &updatedAt)
	if err != nil {
		return nil, translate(operation, err)
	}

	return rehydrateWallet(id, playerID, currencyCode, balanceMinor, version, createdAt, updatedAt)
}

func rehydrateWallet(
	id, playerID, currencyCode string,
	balanceMinor, version int64,
	createdAt, updatedAt time.Time,
) (*wallet.Wallet, error) {
	walletID, err := shared.ParseID(id)
	if err != nil {
		return nil, err
	}
	player, err := shared.ParseID(playerID)
	if err != nil {
		return nil, err
	}
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return nil, err
	}

	return wallet.Rehydrate(wallet.State{
		ID:        walletID,
		PlayerID:  player,
		Balance:   money.FromMinorUnits(balanceMinor, currency),
		Version:   version,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	})
}
