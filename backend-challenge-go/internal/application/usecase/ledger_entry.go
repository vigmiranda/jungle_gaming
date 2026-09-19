package usecase

import (
	"context"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// appendEntry registra o lançamento correspondente a uma movimentação.
//
// A abertura de carteira e o processamento de operações usam o mesmo caminho:
// os três valores do lançamento vêm sempre do agregado, nunca recalculados pela
// camada de aplicação.
func appendEntry(
	ctx context.Context,
	repositories port.Repositories,
	ids port.IDGenerator,
	walletID, transactionID shared.ID,
	direction ledger.Direction,
	movement wallet.Movement,
	now time.Time,
) error {
	entryID, err := ids.NewID()
	if err != nil {
		return err
	}

	entry, err := ledger.NewEntry(
		entryID, walletID, transactionID, direction,
		movement.Amount, movement.BalanceBefore, movement.BalanceAfter, now)
	if err != nil {
		return err
	}
	return repositories.Ledger().Append(ctx, entry)
}
