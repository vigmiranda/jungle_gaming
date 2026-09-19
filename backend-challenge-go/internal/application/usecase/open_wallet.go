package usecase

import (
	"context"
	"errors"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// MoneyInput é o valor monetário como chega do contrato externo.
type MoneyInput struct {
	Amount   string
	Currency string
}

// OpenWalletCommand descreve a abertura de carteira.
type OpenWalletCommand struct {
	PlayerID       string
	InitialBalance MoneyInput
}

// OpenWalletResult é a carteira recém-aberta.
type OpenWalletResult struct {
	Wallet *wallet.Wallet
}

// OpenWallet abre a carteira de um jogador em uma moeda.
type OpenWallet struct {
	unitOfWork port.UnitOfWork
	clock      port.Clock
	ids        port.IDGenerator
}

// NewOpenWallet monta o caso de uso.
func NewOpenWallet(unitOfWork port.UnitOfWork, clock port.Clock, ids port.IDGenerator) *OpenWallet {
	return &OpenWallet{unitOfWork: unitOfWork, clock: clock, ids: ids}
}

// Execute abre a carteira.
//
// Com saldo inicial positivo, a abertura cria a transação `OPENING` já
// processada e o lançamento de crédito no mesmo commit da carteira, e a versão
// permanece 1. Com saldo zero, cria apenas a carteira: sem movimentação não há
// transação nem lançamento.
func (uc *OpenWallet) Execute(ctx context.Context, command OpenWalletCommand) (OpenWalletResult, error) {
	playerID, err := shared.ParseID(command.PlayerID)
	if err != nil {
		return OpenWalletResult{}, ErrInvalidInput.Messagef("playerId inválido").WithCause(err)
	}

	initialBalance, err := money.Parse(command.InitialBalance.Amount, command.InitialBalance.Currency)
	if err != nil {
		return OpenWalletResult{}, err
	}
	if initialBalance.IsNegative() {
		return OpenWalletResult{}, ErrInvalidInput.Messagef(
			"saldo inicial não pode ser negativo: %s", initialBalance)
	}

	walletID, err := uc.ids.NewID()
	if err != nil {
		return OpenWalletResult{}, err
	}

	now := uc.clock.Now()
	opened, err := wallet.Open(walletID, playerID, initialBalance, now)
	if err != nil {
		return OpenWalletResult{}, err
	}

	err = uc.unitOfWork.Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		if err := repositories.Wallets().Create(ctx, opened); err != nil {
			if errors.Is(err, port.ErrConflict) {
				return ErrWalletAlreadyExists.
					Messagef("jogador %s já possui carteira em %s", playerID, initialBalance.Currency()).
					WithCause(err)
			}
			return err
		}

		if initialBalance.IsZero() {
			return nil
		}
		return uc.recordOpening(ctx, repositories, opened)
	})
	if err != nil {
		return OpenWalletResult{}, err
	}

	return OpenWalletResult{Wallet: opened}, nil
}

// recordOpening registra o crédito inicial: transação interna `OPENING` e o
// lançamento correspondente, ambos no commit da carteira.
func (uc *OpenWallet) recordOpening(
	ctx context.Context,
	repositories port.Repositories,
	opened *wallet.Wallet,
) error {
	transactionID, err := uc.ids.NewID()
	if err != nil {
		return err
	}

	opening, err := wagering.NewProcessedOpening(wagering.OpeningParams{
		ID:        transactionID,
		WalletID:  opened.ID(),
		PlayerID:  opened.PlayerID(),
		Amount:    opened.Balance(),
		CreatedAt: opened.CreatedAt(),
	}, wagering.Result{
		Balance:       opened.Balance(),
		WalletVersion: opened.Version(),
	})
	if err != nil {
		return err
	}
	if err := repositories.Transactions().Create(ctx, opening); err != nil {
		return err
	}

	return appendEntry(ctx, repositories, uc.ids,
		opened.ID(), opening.ID(), ledger.Credit, opened.OpeningMovement(), opened.CreatedAt())
}
