package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// ProcessTransactionCommand é a operação como chega do provedor, por HTTP ou
// por SQS. Os campos são strings porque a validação estrita acontece aqui, uma
// única vez, para os dois canais.
type ProcessTransactionCommand struct {
	IdempotencyKey                 string
	ProviderID                     string
	ExternalTransactionID          string
	PlayerID                       string
	WalletID                       string
	RoundID                        string
	GameID                         string
	Kind                           string
	Money                          MoneyInput
	ReferenceExternalTransactionID string
}

// TransactionResult é o resultado devolvido ao provedor.
type TransactionResult struct {
	TransactionID    shared.ID
	Status           wagering.Status
	Balance          money.Money
	HasBalance       bool
	FailureCode      wagering.FailureCode
	IdempotentReplay bool
}

// ProcessWagerTransaction aplica uma operação financeira de provedor.
type ProcessWagerTransaction struct {
	unitOfWork port.UnitOfWork
	clock      port.Clock
	ids        port.IDGenerator
}

// NewProcessWagerTransaction monta o caso de uso.
func NewProcessWagerTransaction(
	unitOfWork port.UnitOfWork,
	clock port.Clock,
	ids port.IDGenerator,
) *ProcessWagerTransaction {
	return &ProcessWagerTransaction{unitOfWork: unitOfWork, clock: clock, ids: ids}
}

// request é o comando já validado e convertido para tipos de domínio.
type request struct {
	command  ProcessTransactionCommand
	hash     string
	kind     wagering.Kind
	walletID shared.ID
	playerID shared.ID
	amount   money.Money
}

// Execute processa a operação.
//
// A ordem dentro da transação é deliberada: o lock da carteira vem antes da
// checagem de idempotência. Assim duas entregas simultâneas da mesma operação
// se enfileiram, a segunda encontra o registro da primeira e devolve replay —
// sem depender de tratar violação de unicidade nem de repetir a transação.
func (uc *ProcessWagerTransaction) Execute(
	ctx context.Context,
	command ProcessTransactionCommand,
) (TransactionResult, error) {
	var result TransactionResult
	err := uc.unitOfWork.Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		var err error
		result, err = uc.ExecuteIn(ctx, repositories, command)
		return err
	})
	if err != nil {
		return TransactionResult{}, err
	}
	return result, nil
}

// ExecuteIn aplica a operação dentro de uma unidade de trabalho já aberta.
//
// Usado pelo consumidor SQS para gravar a inbox no mesmo commit do efeito
// financeiro. HTTP continua chamando `Execute`, que abre a transação.
func (uc *ProcessWagerTransaction) ExecuteIn(
	ctx context.Context,
	repositories port.Repositories,
	command ProcessTransactionCommand,
) (TransactionResult, error) {
	parsed, err := uc.parse(command)
	if err != nil {
		return TransactionResult{}, err
	}

	target, err := repositories.Wallets().LockByID(ctx, parsed.walletID)
	if err != nil {
		return TransactionResult{}, err
	}

	replay, found, err := uc.findReplay(ctx, repositories, parsed)
	if err != nil {
		return TransactionResult{}, err
	}
	if found {
		return replay, nil
	}

	return uc.apply(ctx, repositories, target, parsed)
}

func (uc *ProcessWagerTransaction) parse(command ProcessTransactionCommand) (request, error) {
	if command.IdempotencyKey == "" {
		return request{}, ErrInvalidInput.Messagef("Idempotency-Key é obrigatório")
	}
	if command.ProviderID == "" {
		return request{}, ErrInvalidInput.Messagef("providerId é obrigatório")
	}
	if command.ExternalTransactionID == "" {
		return request{}, ErrInvalidInput.Messagef("externalTransactionId é obrigatório")
	}

	kind, err := wagering.ParseExternalKind(command.Kind)
	if err != nil {
		return request{}, err
	}
	walletID, err := shared.ParseID(command.WalletID)
	if err != nil {
		return request{}, ErrInvalidInput.Messagef("walletId inválido").WithCause(err)
	}
	playerID, err := shared.ParseID(command.PlayerID)
	if err != nil {
		return request{}, ErrInvalidInput.Messagef("playerId inválido").WithCause(err)
	}
	amount, err := money.Parse(command.Money.Amount, command.Money.Currency)
	if err != nil {
		return request{}, err
	}
	if err := kind.ValidateAmount(amount); err != nil {
		return request{}, err
	}
	if err := kind.ValidateReference(command.ReferenceExternalTransactionID); err != nil {
		return request{}, err
	}

	return request{
		command:  command,
		hash:     payloadHash(command),
		kind:     kind,
		walletID: walletID,
		playerID: playerID,
		amount:   amount,
	}, nil
}

// findReplay resolve reenvio e conflito antes de qualquer efeito financeiro.
func (uc *ProcessWagerTransaction) findReplay(
	ctx context.Context,
	repositories port.Repositories,
	parsed request,
) (TransactionResult, bool, error) {
	transactions := repositories.Transactions()

	existing, err := transactions.FindByIdempotencyKey(ctx, parsed.command.ProviderID, parsed.command.IdempotencyKey)
	switch {
	case err == nil:
		if existing.PayloadHash() != parsed.hash {
			return TransactionResult{}, false, ErrIdempotencyConflict.Messagef(
				"chave %q já registrada com outro conteúdo", parsed.command.IdempotencyKey)
		}
		return replayOf(existing), true, nil
	case !errors.Is(err, port.ErrNotFound):
		return TransactionResult{}, false, err
	}

	// Chave nova apontando para uma operação já registrada: a mesma operação
	// financeira não pode ser reaplicada sob outra chave.
	applied, err := transactions.FindByExternalID(ctx, parsed.command.ProviderID, parsed.command.ExternalTransactionID)
	switch {
	case err == nil:
		return TransactionResult{}, false, ErrOperationReapplied.Messagef(
			"operação %s/%s já registrada com a chave %q",
			parsed.command.ProviderID, parsed.command.ExternalTransactionID, applied.IdempotencyKey())
	case !errors.Is(err, port.ErrNotFound):
		return TransactionResult{}, false, err
	}

	return TransactionResult{}, false, nil
}

// replayOf reconstrói o resultado a partir do que foi persistido.
//
// O saldo vem do resultado congelado no processamento original, não do saldo
// atual da carteira: replays posteriores a outras movimentações devolvem o
// mesmo valor que o provedor viu na primeira vez (ADR-014).
func replayOf(existing *wagering.Transaction) TransactionResult {
	result := TransactionResult{
		TransactionID:    existing.ID(),
		Status:           existing.Status(),
		FailureCode:      existing.FailureCode(),
		IdempotentReplay: true,
	}
	if persisted, ok := existing.Result(); ok {
		result.Balance = persisted.Balance
		result.HasBalance = true
	}
	return result
}

func (uc *ProcessWagerTransaction) apply(
	ctx context.Context,
	repositories port.Repositories,
	target *wallet.Wallet,
	parsed request,
) (TransactionResult, error) {
	transactionID, err := uc.ids.NewID()
	if err != nil {
		return TransactionResult{}, err
	}

	now := uc.clock.Now()
	transaction, err := wagering.NewExternal(wagering.ExternalParams{
		ID:                  transactionID,
		Kind:                parsed.kind,
		WalletID:            parsed.walletID,
		PlayerID:            parsed.playerID,
		Amount:              parsed.amount,
		ProviderID:          parsed.command.ProviderID,
		ExternalID:          parsed.command.ExternalTransactionID,
		IdempotencyKey:      parsed.command.IdempotencyKey,
		PayloadHash:         parsed.hash,
		RoundID:             parsed.command.RoundID,
		GameID:              parsed.command.GameID,
		ReferenceExternalID: parsed.command.ReferenceExternalTransactionID,
		CreatedAt:           now,
	})
	if err != nil {
		return TransactionResult{}, err
	}

	if !target.PlayerID().Equal(parsed.playerID) {
		return uc.reject(ctx, repositories, transaction, wagering.FailureWalletPlayerMismatch, now)
	}
	if target.Currency() != parsed.amount.Currency() {
		return uc.reject(ctx, repositories, transaction, wagering.FailureCurrencyMismatch, now)
	}

	if parsed.kind.IsReversal() {
		return uc.applyReversal(ctx, repositories, target, transaction, parsed, now)
	}
	return uc.applyDirect(ctx, repositories, target, transaction, parsed, now)
}

// applyDirect trata os tipos que não dependem de referência.
func (uc *ProcessWagerTransaction) applyDirect(
	ctx context.Context,
	repositories port.Repositories,
	target *wallet.Wallet,
	transaction *wagering.Transaction,
	parsed request,
	now time.Time,
) (TransactionResult, error) {
	switch parsed.kind {
	case wagering.Bet:
		return uc.move(ctx, repositories, target, transaction, ledger.Debit, parsed.amount, now)
	case wagering.Win:
		return uc.move(ctx, repositories, target, transaction, ledger.Credit, parsed.amount, now)
	default:
		// LOSS não movimenta saldo: sem lançamento e sem incremento de versão.
		return uc.settle(ctx, repositories, target, transaction, nil, now)
	}
}

// applyReversal trata `REFUND` e `ROLLBACK`, que dependem de uma operação
// anterior do mesmo provedor.
func (uc *ProcessWagerTransaction) applyReversal(
	ctx context.Context,
	repositories port.Repositories,
	target *wallet.Wallet,
	transaction *wagering.Transaction,
	parsed request,
	now time.Time,
) (TransactionResult, error) {
	reference, err := repositories.Transactions().FindByExternalID(
		ctx, parsed.command.ProviderID, parsed.command.ReferenceExternalTransactionID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			// A reversão chegou antes da operação referenciada. Fica pendente e
			// o worker de referências continua depois (ADR-009).
			return uc.markPending(ctx, repositories, transaction, now)
		}
		return TransactionResult{}, err
	}

	// Referência ainda em voo: esperar é o que sustenta a entrega fora de ordem.
	if !reference.Status().IsTerminal() {
		return uc.markPending(ctx, repositories, transaction, now)
	}
	if code, ok := reversalRejection(reference, parsed); ok {
		return uc.reject(ctx, repositories, transaction, code, now)
	}

	// Uma operação é revertida no máximo uma vez, por qualquer tipo: um REFUND
	// seguido de um ROLLBACK da mesma aposta devolveria o débito duas vezes.
	reverted, err := repositories.Transactions().HasSuccessfulReversal(ctx, reference.ID())
	if err != nil {
		return TransactionResult{}, err
	}
	if reverted {
		return uc.reject(ctx, repositories, transaction, wagering.FailureDuplicateReversal, now)
	}

	if err := transaction.ResolveReference(reference.ID(), now); err != nil {
		return TransactionResult{}, err
	}

	direction := ledger.Credit
	if reference.Kind() != wagering.Bet {
		// Desfazer um crédito (WIN ou REFUND) significa debitar de volta.
		direction = ledger.Debit
	}
	return uc.move(ctx, repositories, target, transaction, direction, parsed.amount, now)
}

// reversalRejection avalia a elegibilidade da referência.
func reversalRejection(reference *wagering.Transaction, parsed request) (wagering.FailureCode, bool) {
	if reference.Status() != wagering.Processed {
		return wagering.FailureReferenceNotProcessed, true
	}
	if !eligibleForReversal(parsed.kind, reference.Kind()) {
		return wagering.FailureReferenceNotProcessed, true
	}
	// A operação e sua referência precisam concordar em jogador, carteira,
	// rodada e valor; o provedor já coincide pela forma da consulta.
	if !reference.WalletID().Equal(parsed.walletID) ||
		!reference.PlayerID().Equal(parsed.playerID) ||
		reference.RoundID() != parsed.command.RoundID ||
		!reference.Amount().Equal(parsed.amount) {
		return wagering.FailureReferenceMismatch, true
	}
	return "", false
}

// eligibleForReversal define o que cada reversão pode desfazer: `REFUND` é
// exclusivo de aposta, `ROLLBACK` desfaz qualquer movimentação anterior.
func eligibleForReversal(reversal, referenced wagering.Kind) bool {
	if reversal == wagering.Refund {
		return referenced == wagering.Bet
	}
	return referenced == wagering.Bet || referenced == wagering.Win || referenced == wagering.Refund
}

// move aplica a movimentação no agregado e registra o lançamento.
func (uc *ProcessWagerTransaction) move(
	ctx context.Context,
	repositories port.Repositories,
	target *wallet.Wallet,
	transaction *wagering.Transaction,
	direction ledger.Direction,
	amount money.Money,
	now time.Time,
) (TransactionResult, error) {
	var (
		movement wallet.Movement
		err      error
	)
	if direction == ledger.Credit {
		movement, err = target.Credit(amount, now)
	} else {
		movement, err = target.Debit(amount, now)
	}
	if err != nil {
		if errors.Is(err, wallet.ErrInsufficientFunds) {
			return uc.reject(ctx, repositories, transaction, insufficientFundsCode(transaction.Kind()), now)
		}
		return TransactionResult{}, err
	}

	return uc.settle(ctx, repositories, target, transaction, &settlement{
		direction: direction,
		movement:  movement,
	}, now)
}

// insufficientFundsCode distingue a aposta sem saldo da reversão que deixaria
// o saldo negativo: causas diferentes exigem ações corretivas diferentes.
func insufficientFundsCode(kind wagering.Kind) wagering.FailureCode {
	if kind.IsReversal() {
		return wagering.FailureReversalExceedsBalance
	}
	return wagering.FailureInsufficientFunds
}

// settlement descreve a movimentação aplicada, quando houve.
type settlement struct {
	direction ledger.Direction
	movement  wallet.Movement
}

// settle conclui a operação, gravando transação, lançamento e saldo no mesmo
// commit.
func (uc *ProcessWagerTransaction) settle(
	ctx context.Context,
	repositories port.Repositories,
	target *wallet.Wallet,
	transaction *wagering.Transaction,
	applied *settlement,
	now time.Time,
) (TransactionResult, error) {
	if err := transaction.MarkProcessed(wagering.Result{
		Balance:       target.Balance(),
		WalletVersion: target.Version(),
	}, now); err != nil {
		return TransactionResult{}, err
	}
	if err := repositories.Transactions().Create(ctx, transaction); err != nil {
		return TransactionResult{}, err
	}

	if applied != nil {
		err := appendEntry(ctx, repositories, uc.ids,
			target.ID(), transaction.ID(), applied.direction, applied.movement, now)
		if err != nil {
			return TransactionResult{}, err
		}
		if err := repositories.Wallets().UpdateBalance(ctx, target); err != nil {
			return TransactionResult{}, err
		}
	}

	if err := appendProcessedEvents(ctx, repositories, uc.ids, transaction, applied, now); err != nil {
		return TransactionResult{}, err
	}

	return TransactionResult{
		TransactionID: transaction.ID(),
		Status:        transaction.Status(),
		Balance:       target.Balance(),
		HasBalance:    true,
	}, nil
}

// reject registra a recusa de negócio como estado terminal.
//
// A rejeição é persistida, e não desfeita: um reenvio precisa devolver a mesma
// recusa, com o mesmo código, em vez de tentar a operação outra vez.
func (uc *ProcessWagerTransaction) reject(
	ctx context.Context,
	repositories port.Repositories,
	transaction *wagering.Transaction,
	code wagering.FailureCode,
	now time.Time,
) (TransactionResult, error) {
	if err := transaction.Reject(code, now); err != nil {
		return TransactionResult{}, err
	}
	if err := repositories.Transactions().Create(ctx, transaction); err != nil {
		return TransactionResult{}, err
	}
	if err := appendRejectedEvent(ctx, repositories, uc.ids, transaction, now); err != nil {
		return TransactionResult{}, err
	}

	return TransactionResult{
		TransactionID: transaction.ID(),
		Status:        transaction.Status(),
		FailureCode:   code,
	}, nil
}

// markPending registra a espera pela referência.
func (uc *ProcessWagerTransaction) markPending(
	ctx context.Context,
	repositories port.Repositories,
	transaction *wagering.Transaction,
	now time.Time,
) (TransactionResult, error) {
	if err := transaction.MarkPendingReference(now); err != nil {
		return TransactionResult{}, err
	}
	if err := repositories.Transactions().Create(ctx, transaction); err != nil {
		return TransactionResult{}, err
	}
	if err := appendPendingReferenceEvent(ctx, repositories, uc.ids, transaction, now); err != nil {
		return TransactionResult{}, err
	}

	return TransactionResult{
		TransactionID: transaction.ID(),
		Status:        transaction.Status(),
	}, nil
}
