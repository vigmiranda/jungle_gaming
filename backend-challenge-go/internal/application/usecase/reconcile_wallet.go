package usecase

import (
	"context"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// ReconciliationResult compara o saldo armazenado com o reconstruído.
type ReconciliationResult struct {
	WalletID          shared.ID
	StoredBalance     money.Money
	CalculatedBalance money.Money
	Difference        money.Money
	Consistent        bool
	CheckedEntries    int
}

// ReconcileWallet reconstrói o saldo a partir do ledger e compara com o
// armazenado.
type ReconcileWallet struct {
	unitOfWork port.UnitOfWork
}

// NewReconcileWallet monta o caso de uso.
func NewReconcileWallet(unitOfWork port.UnitOfWork) *ReconcileWallet {
	return &ReconcileWallet{unitOfWork: unitOfWork}
}

// Execute reconcilia a carteira.
//
// A leitura acontece em uma transação somente leitura com isolamento
// `REPEATABLE READ`: saldo e ledger precisam vir da mesma visão dos dados, ou
// uma movimentação concorrente apareceria como divergência inexistente.
//
// A operação não altera saldo: divergência é reportada, nunca corrigida em
// silêncio. Correção financeira exige lançamento novo.
func (uc *ReconcileWallet) Execute(ctx context.Context, walletID shared.ID) (ReconciliationResult, error) {
	var result ReconciliationResult

	err := uc.unitOfWork.ExecuteReadOnly(ctx, func(ctx context.Context, repositories port.Repositories) error {
		target, err := repositories.Wallets().FindByID(ctx, walletID)
		if err != nil {
			return err
		}

		calculated, entries, err := repositories.Ledger().SumByWallet(ctx, walletID)
		if err != nil {
			return err
		}
		// Sem lançamentos não há moeda a inferir do ledger; o zero da carteira
		// é a comparação correta.
		if entries == 0 {
			calculated = money.Zero(target.Currency())
		}

		difference, err := target.Balance().Sub(calculated)
		if err != nil {
			return err
		}

		result = ReconciliationResult{
			WalletID:          walletID,
			StoredBalance:     target.Balance(),
			CalculatedBalance: calculated,
			Difference:        difference,
			Consistent:        difference.IsZero(),
			CheckedEntries:    entries,
		}
		return nil
	})
	if err != nil {
		return ReconciliationResult{}, err
	}
	return result, nil
}
