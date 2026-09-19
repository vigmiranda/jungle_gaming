package repository

import (
	"context"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

// TransactionRepository persiste a operação de aposta.
type TransactionRepository struct {
	db querier
}

const transactionColumns = `
	id, origin, kind, status, wallet_id, player_id, amount_minor, amount_currency,
	provider_id, external_transaction_id, idempotency_key, payload_hash,
	round_id, game_id, reference_external_transaction_id, reference_transaction_id,
	failure_code, result_balance_minor, result_balance_currency, result_wallet_version,
	created_at, updated_at`

// Create registra a operação.
func (r *TransactionRepository) Create(ctx context.Context, target *wagering.Transaction) error {
	row := toTransactionRow(target)

	_, err := r.db.Exec(ctx, `
		INSERT INTO wager_transactions (`+transactionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`,
		row.id, row.origin, row.kind, row.status, row.walletID, row.playerID, row.amountMinor, row.amountCurrency,
		row.providerID, row.externalID, row.idempotencyKey, row.payloadHash,
		row.roundID, row.gameID, row.referenceExternalID, row.referenceID,
		row.failureCode, row.resultBalanceMinor, row.resultBalanceCurrency, row.resultWalletVersion,
		row.createdAt, row.updatedAt,
	)
	return translate("criar transação", err)
}

// Update grava a transição de estado e o resultado do processamento.
//
// A identidade e os metadados de entrada são imutáveis: mudam apenas estado,
// referência resolvida, código de falha, resultado e o instante de atualização.
func (r *TransactionRepository) Update(ctx context.Context, target *wagering.Transaction) error {
	row := toTransactionRow(target)

	tag, err := r.db.Exec(ctx, `
		UPDATE wager_transactions
		SET status = $2,
		    reference_transaction_id = $3,
		    failure_code = $4,
		    result_balance_minor = $5,
		    result_balance_currency = $6,
		    result_wallet_version = $7,
		    updated_at = $8
		WHERE id = $1`,
		row.id, row.status, row.referenceID, row.failureCode,
		row.resultBalanceMinor, row.resultBalanceCurrency, row.resultWalletVersion,
		row.updatedAt,
	)
	if err != nil {
		return translate("atualizar transação", err)
	}
	return requireSingleRow("atualizar transação", tag)
}

// FindByID carrega a operação pelo identificador interno.
func (r *TransactionRepository) FindByID(ctx context.Context, id shared.ID) (*wagering.Transaction, error) {
	return r.queryOne(ctx, "consultar transação", `
		SELECT `+transactionColumns+`
		FROM wager_transactions
		WHERE id = $1`, id.String())
}

// FindByIdempotencyKey resolve replay e conflito de chave, no escopo do provedor.
func (r *TransactionRepository) FindByIdempotencyKey(
	ctx context.Context,
	providerID, idempotencyKey string,
) (*wagering.Transaction, error) {
	return r.queryOne(ctx, "consultar por chave de idempotência", `
		SELECT `+transactionColumns+`
		FROM wager_transactions
		WHERE provider_id = $1 AND idempotency_key = $2`, providerID, idempotencyKey)
}

// FindByExternalID impede que a mesma operação financeira seja reaplicada com
// outra chave de idempotência.
func (r *TransactionRepository) FindByExternalID(
	ctx context.Context,
	providerID, externalID string,
) (*wagering.Transaction, error) {
	return r.queryOne(ctx, "consultar por id externo", `
		SELECT `+transactionColumns+`
		FROM wager_transactions
		WHERE provider_id = $1 AND external_transaction_id = $2`, providerID, externalID)
}

// transactionRow espelha as colunas, com ponteiros onde o schema aceita nulo.
type transactionRow struct {
	id                    string
	origin                string
	kind                  string
	status                string
	walletID              string
	playerID              string
	amountMinor           int64
	amountCurrency        string
	providerID            *string
	externalID            *string
	idempotencyKey        *string
	payloadHash           *string
	roundID               *string
	gameID                *string
	referenceExternalID   *string
	referenceID           *string
	failureCode           *string
	resultBalanceMinor    *int64
	resultBalanceCurrency *string
	resultWalletVersion   *int64
	createdAt             time.Time
	updatedAt             time.Time
}

func toTransactionRow(target *wagering.Transaction) transactionRow {
	row := transactionRow{
		id:             target.ID().String(),
		origin:         string(target.Origin()),
		kind:           target.Kind().String(),
		status:         target.Status().String(),
		walletID:       target.WalletID().String(),
		playerID:       target.PlayerID().String(),
		amountMinor:    target.Amount().MinorUnits(),
		amountCurrency: target.Amount().Currency().String(),
		createdAt:      target.CreatedAt(),
		updatedAt:      target.UpdatedAt(),
	}

	row.providerID = optionalText(target.ProviderID())
	row.externalID = optionalText(target.ExternalID())
	row.idempotencyKey = optionalText(target.IdempotencyKey())
	row.payloadHash = optionalText(target.PayloadHash())
	row.roundID = optionalText(target.RoundID())
	row.gameID = optionalText(target.GameID())
	row.referenceExternalID = optionalText(target.ReferenceExternalID())
	row.failureCode = optionalText(target.FailureCode().String())

	if referenceID, ok := target.ReferenceID(); ok {
		row.referenceID = optionalText(referenceID.String())
	}
	if result, ok := target.Result(); ok {
		minor := result.Balance.MinorUnits()
		currency := result.Balance.Currency().String()
		version := result.WalletVersion
		row.resultBalanceMinor = &minor
		row.resultBalanceCurrency = &currency
		row.resultWalletVersion = &version
	}
	return row
}

// optionalText converte o vazio do domínio no NULL do schema, preservando a
// distinção que as constraints de origem impõem.
func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (r *TransactionRepository) queryOne(
	ctx context.Context,
	operation, query string,
	args ...any,
) (*wagering.Transaction, error) {
	var row transactionRow

	err := r.db.QueryRow(ctx, query, args...).Scan(
		&row.id, &row.origin, &row.kind, &row.status, &row.walletID, &row.playerID,
		&row.amountMinor, &row.amountCurrency,
		&row.providerID, &row.externalID, &row.idempotencyKey, &row.payloadHash,
		&row.roundID, &row.gameID, &row.referenceExternalID, &row.referenceID,
		&row.failureCode, &row.resultBalanceMinor, &row.resultBalanceCurrency, &row.resultWalletVersion,
		&row.createdAt, &row.updatedAt,
	)
	if err != nil {
		return nil, translate(operation, err)
	}
	return row.toDomain()
}

func (row transactionRow) toDomain() (*wagering.Transaction, error) {
	id, err := shared.ParseID(row.id)
	if err != nil {
		return nil, err
	}
	walletID, err := shared.ParseID(row.walletID)
	if err != nil {
		return nil, err
	}
	playerID, err := shared.ParseID(row.playerID)
	if err != nil {
		return nil, err
	}
	kind, err := wagering.ParseKind(row.kind)
	if err != nil {
		return nil, err
	}
	status, err := wagering.ParseStatus(row.status)
	if err != nil {
		return nil, err
	}
	currency, err := money.ParseCurrency(row.amountCurrency)
	if err != nil {
		return nil, err
	}

	state := wagering.State{
		ID:                  id,
		Origin:              wagering.Origin(row.origin),
		Kind:                kind,
		WalletID:            walletID,
		PlayerID:            playerID,
		Amount:              money.FromMinorUnits(row.amountMinor, currency),
		Status:              status,
		ProviderID:          text(row.providerID),
		ExternalID:          text(row.externalID),
		IdempotencyKey:      text(row.idempotencyKey),
		PayloadHash:         text(row.payloadHash),
		RoundID:             text(row.roundID),
		GameID:              text(row.gameID),
		ReferenceExternalID: text(row.referenceExternalID),
		FailureCode:         wagering.FailureCode(text(row.failureCode)),
		CreatedAt:           row.createdAt,
		UpdatedAt:           row.updatedAt,
	}

	if row.referenceID != nil {
		referenceID, err := shared.ParseID(*row.referenceID)
		if err != nil {
			return nil, err
		}
		state.ReferenceID = &referenceID
	}

	if row.resultBalanceMinor != nil && row.resultBalanceCurrency != nil && row.resultWalletVersion != nil {
		resultCurrency, err := money.ParseCurrency(*row.resultBalanceCurrency)
		if err != nil {
			return nil, err
		}
		state.Result = &wagering.Result{
			Balance:       money.FromMinorUnits(*row.resultBalanceMinor, resultCurrency),
			WalletVersion: *row.resultWalletVersion,
		}
	}

	return wagering.Rehydrate(state)
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
