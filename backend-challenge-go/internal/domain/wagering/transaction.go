// Package wagering implementa a transação de aposta e sua máquina de estados.
//
// A transação distingue duas origens: interna (`OPENING`, gerada pela abertura
// de carteira) e externa (os cinco tipos enviados por provedores). Os metadados
// externos — provedor, chave de idempotência, hash, rodada e jogo — não se
// aplicam à origem interna.
package wagering

import (
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// Erros da transação.
var (
	// ErrMissingField cobre metadados obrigatórios ausentes.
	ErrMissingField = shared.Validation("MISSING_FIELD", "campo obrigatório ausente")
	// ErrInvalidTransactionState cobre reidratação inconsistente.
	ErrInvalidTransactionState = shared.Invariant("INVALID_TRANSACTION_STATE", "estado de transação inválido")
)

// Origin indica de onde a operação veio.
type Origin string

// Origens possíveis.
const (
	// OriginInternal é a abertura de carteira gerada pela própria aplicação.
	OriginInternal Origin = "INTERNAL"
	// OriginExternal é a operação enviada por um provedor, via HTTP ou SQS.
	OriginExternal Origin = "EXTERNAL"
)

// Result é o resultado financeiro devolvido ao provedor.
//
// Fica persistido na transação para que o replay reproduza o saldo observado no
// processamento original, mesmo que a carteira tenha se movimentado depois
// (ADR-014).
type Result struct {
	Balance       money.Money
	WalletVersion int64
}

// Transaction é a operação registrada, com sua máquina de estados.
type Transaction struct {
	id       shared.ID
	origin   Origin
	kind     Kind
	walletID shared.ID
	playerID shared.ID
	amount   money.Money
	status   Status

	providerID          string
	externalID          string
	idempotencyKey      string
	payloadHash         string
	roundID             string
	gameID              string
	referenceExternalID string

	referenceID *shared.ID
	failureCode FailureCode
	result      *Result

	createdAt time.Time
	updatedAt time.Time
}

// ExternalParams reúne os dados de uma operação recebida de um provedor.
type ExternalParams struct {
	ID                  shared.ID
	Kind                Kind
	WalletID            shared.ID
	PlayerID            shared.ID
	Amount              money.Money
	ProviderID          string
	ExternalID          string
	IdempotencyKey      string
	PayloadHash         string
	RoundID             string
	GameID              string
	ReferenceExternalID string
	CreatedAt           time.Time
}

// NewExternal registra uma operação de provedor em `PENDING`.
//
// O estado inicial é sempre `PENDING`: a conclusão acontece na mesma unidade de
// trabalho (ADR-012), exceto quando falta a referência.
func NewExternal(params ExternalParams) (*Transaction, error) {
	if err := params.ID.Validate(); err != nil {
		return nil, err
	}
	if err := params.WalletID.Validate(); err != nil {
		return nil, err
	}
	if err := params.PlayerID.Validate(); err != nil {
		return nil, err
	}
	if _, err := ParseExternalKind(params.Kind.String()); err != nil {
		return nil, err
	}
	if err := params.Kind.ValidateAmount(params.Amount); err != nil {
		return nil, err
	}
	if err := params.Kind.ValidateReference(params.ReferenceExternalID); err != nil {
		return nil, err
	}
	required := map[string]string{
		"providerId":            params.ProviderID,
		"externalTransactionId": params.ExternalID,
		"idempotencyKey":        params.IdempotencyKey,
		"payloadHash":           params.PayloadHash,
		"roundId":               params.RoundID,
		"gameId":                params.GameID,
	}
	for field, value := range required {
		if value == "" {
			return nil, ErrMissingField.Messagef("%s é obrigatório em operações externas", field)
		}
	}
	if params.CreatedAt.IsZero() {
		return nil, ErrMissingField.Messagef("instante de criação não informado")
	}

	return &Transaction{
		id:                  params.ID,
		origin:              OriginExternal,
		kind:                params.Kind,
		walletID:            params.WalletID,
		playerID:            params.PlayerID,
		amount:              params.Amount,
		status:              Pending,
		providerID:          params.ProviderID,
		externalID:          params.ExternalID,
		idempotencyKey:      params.IdempotencyKey,
		payloadHash:         params.PayloadHash,
		roundID:             params.RoundID,
		gameID:              params.GameID,
		referenceExternalID: params.ReferenceExternalID,
		createdAt:           params.CreatedAt.UTC(),
		updatedAt:           params.CreatedAt.UTC(),
	}, nil
}

// OpeningParams reúne os dados da abertura interna de carteira.
type OpeningParams struct {
	ID        shared.ID
	WalletID  shared.ID
	PlayerID  shared.ID
	Amount    money.Money
	CreatedAt time.Time
}

// NewOpening registra o crédito inicial de uma carteira.
//
// Só faz sentido com valor positivo: abertura com saldo zero não cria `OPENING`,
// ledger nem eventos financeiros.
func NewOpening(params OpeningParams) (*Transaction, error) {
	if err := params.ID.Validate(); err != nil {
		return nil, err
	}
	if err := params.WalletID.Validate(); err != nil {
		return nil, err
	}
	if err := params.PlayerID.Validate(); err != nil {
		return nil, err
	}
	if err := params.Amount.Validate(); err != nil {
		return nil, err
	}
	if !params.Amount.IsPositive() {
		return nil, ErrInvalidAmountForKind.Messagef(
			"abertura exige valor maior que zero, recebido %s", params.Amount)
	}
	if params.CreatedAt.IsZero() {
		return nil, ErrMissingField.Messagef("instante de criação não informado")
	}

	return &Transaction{
		id:        params.ID,
		origin:    OriginInternal,
		kind:      Opening,
		walletID:  params.WalletID,
		playerID:  params.PlayerID,
		amount:    params.Amount,
		status:    Pending,
		createdAt: params.CreatedAt.UTC(),
		updatedAt: params.CreatedAt.UTC(),
	}, nil
}

// NewProcessedOpening cria a abertura já concluída.
//
// A abertura com crédito nasce em `PROCESSED` no mesmo commit da carteira, sem
// estado intermediário observável. Criar e concluir em um passo mantém essa
// regra dentro do domínio, em vez de depender de o chamador lembrar da segunda
// chamada.
func NewProcessedOpening(params OpeningParams, result Result) (*Transaction, error) {
	opening, err := NewOpening(params)
	if err != nil {
		return nil, err
	}
	if err := opening.MarkProcessed(result, params.CreatedAt); err != nil {
		return nil, err
	}
	return opening, nil
}

// MarkProcessed conclui a operação com sucesso.
func (t *Transaction) MarkProcessed(result Result, now time.Time) error {
	if err := result.Balance.Validate(); err != nil {
		return err
	}
	if result.WalletVersion < 1 {
		return ErrInvalidTransactionState.Messagef("versão da carteira inválida: %d", result.WalletVersion)
	}
	if err := t.transition(Processed, now); err != nil {
		return err
	}
	t.result = &result
	return nil
}

// MarkPendingReference registra a espera por uma referência ainda indisponível.
func (t *Transaction) MarkPendingReference(now time.Time) error {
	if !t.kind.IsReversal() {
		return ErrIllegalTransition.Messagef("%s não depende de referência", t.kind)
	}
	return t.transition(PendingReference, now)
}

// Reject encerra a operação por regra de negócio.
func (t *Transaction) Reject(code FailureCode, now time.Time) error {
	if code == "" {
		return ErrMissingField.Messagef("rejeição exige um failureCode")
	}
	if err := t.transition(Rejected, now); err != nil {
		return err
	}
	t.failureCode = code
	return nil
}

// Fail encerra a operação por falha permanente de infraestrutura, preservando o
// registro para auditoria (ADR-016).
func (t *Transaction) Fail(code FailureCode, now time.Time) error {
	if code == "" {
		return ErrMissingField.Messagef("falha exige um failureCode")
	}
	if err := t.transition(Failed, now); err != nil {
		return err
	}
	t.failureCode = code
	return nil
}

// ResolveReference registra a transação interna referenciada por uma reversão.
func (t *Transaction) ResolveReference(referenceID shared.ID, now time.Time) error {
	if !t.kind.IsReversal() {
		return ErrReferenceNotAllowed.Messagef("%s não resolve referência", t.kind)
	}
	if err := referenceID.Validate(); err != nil {
		return err
	}
	if t.status.IsTerminal() {
		return ErrIllegalTransition.Messagef("transação em %s não aceita alterações", t.status)
	}
	if now.IsZero() {
		return ErrMissingField.Messagef("instante da resolução não informado")
	}

	resolved := referenceID
	t.referenceID = &resolved
	t.updatedAt = now.UTC()
	return nil
}

func (t *Transaction) transition(target Status, now time.Time) error {
	if now.IsZero() {
		return ErrMissingField.Messagef("instante da transição não informado")
	}
	if !t.status.CanTransitionTo(target) {
		return ErrIllegalTransition.Messagef("transição de %s para %s", t.status, target)
	}
	t.status = target
	t.updatedAt = now.UTC()
	return nil
}

// State é a fotografia persistida da transação, usada na reidratação.
type State struct {
	ID                  shared.ID
	Origin              Origin
	Kind                Kind
	WalletID            shared.ID
	PlayerID            shared.ID
	Amount              money.Money
	Status              Status
	ProviderID          string
	ExternalID          string
	IdempotencyKey      string
	PayloadHash         string
	RoundID             string
	GameID              string
	ReferenceExternalID string
	ReferenceID         *shared.ID
	FailureCode         FailureCode
	Result              *Result
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Rehydrate reconstrói a transação a partir do estado persistido.
//
// A reidratação não reaplica movimentações nem transições: um replay apenas lê
// o resultado já registrado.
func Rehydrate(state State) (*Transaction, error) {
	if err := state.ID.Validate(); err != nil {
		return nil, err
	}
	if err := state.WalletID.Validate(); err != nil {
		return nil, err
	}
	if err := state.PlayerID.Validate(); err != nil {
		return nil, err
	}
	if _, err := ParseKind(state.Kind.String()); err != nil {
		return nil, err
	}
	if _, err := ParseStatus(state.Status.String()); err != nil {
		return nil, err
	}
	if err := state.Amount.Validate(); err != nil {
		return nil, err
	}
	if err := requireOriginConsistency(state); err != nil {
		return nil, err
	}
	if state.CreatedAt.IsZero() || state.UpdatedAt.IsZero() {
		return nil, ErrInvalidTransactionState.Messagef("instantes de criação e atualização são obrigatórios")
	}

	return &Transaction{
		id:                  state.ID,
		origin:              state.Origin,
		kind:                state.Kind,
		walletID:            state.WalletID,
		playerID:            state.PlayerID,
		amount:              state.Amount,
		status:              state.Status,
		providerID:          state.ProviderID,
		externalID:          state.ExternalID,
		idempotencyKey:      state.IdempotencyKey,
		payloadHash:         state.PayloadHash,
		roundID:             state.RoundID,
		gameID:              state.GameID,
		referenceExternalID: state.ReferenceExternalID,
		referenceID:         state.ReferenceID,
		failureCode:         state.FailureCode,
		result:              state.Result,
		createdAt:           state.CreatedAt.UTC(),
		updatedAt:           state.UpdatedAt.UTC(),
	}, nil
}

// requireOriginConsistency impede que metadados externos apareçam na origem
// interna e vice-versa — a mesma distinção imposta pelo schema.
func requireOriginConsistency(state State) error {
	switch state.Origin {
	case OriginInternal:
		if state.Kind != Opening {
			return ErrInvalidTransactionState.Messagef("origem interna só admite OPENING, recebido %s", state.Kind)
		}
		if state.ProviderID != "" || state.ExternalID != "" || state.IdempotencyKey != "" ||
			state.PayloadHash != "" || state.RoundID != "" || state.GameID != "" ||
			state.ReferenceExternalID != "" {
			return ErrInvalidTransactionState.Messagef("origem interna não admite metadados externos")
		}
		return nil
	case OriginExternal:
		if state.Kind == Opening {
			return ErrInvalidTransactionState.Messagef("OPENING não pode ter origem externa")
		}
		if state.ProviderID == "" || state.ExternalID == "" || state.IdempotencyKey == "" ||
			state.PayloadHash == "" {
			return ErrInvalidTransactionState.Messagef("origem externa exige provedor, id externo, chave e hash")
		}
		return nil
	default:
		return ErrInvalidTransactionState.Messagef("origem %q não é suportada", state.Origin)
	}
}

// ID devolve o identificador interno.
func (t *Transaction) ID() shared.ID { return t.id }

// Origin devolve a origem da operação.
func (t *Transaction) Origin() Origin { return t.origin }

// Kind devolve o tipo da operação.
func (t *Transaction) Kind() Kind { return t.kind }

// WalletID devolve a carteira movimentada.
func (t *Transaction) WalletID() shared.ID { return t.walletID }

// PlayerID devolve o jogador.
func (t *Transaction) PlayerID() shared.ID { return t.playerID }

// Amount devolve o valor da operação.
func (t *Transaction) Amount() money.Money { return t.amount }

// Status devolve o estado atual.
func (t *Transaction) Status() Status { return t.status }

// ProviderID devolve o provedor, vazio na origem interna.
func (t *Transaction) ProviderID() string { return t.providerID }

// ExternalID devolve o identificador do provedor, vazio na origem interna.
func (t *Transaction) ExternalID() string { return t.externalID }

// IdempotencyKey devolve a chave de idempotência recebida.
func (t *Transaction) IdempotencyKey() string { return t.idempotencyKey }

// PayloadHash devolve o hash canônico dos campos de negócio.
func (t *Transaction) PayloadHash() string { return t.payloadHash }

// RoundID devolve a rodada.
func (t *Transaction) RoundID() string { return t.roundID }

// GameID devolve o jogo.
func (t *Transaction) GameID() string { return t.gameID }

// ReferenceExternalID devolve a referência informada pelo provedor.
func (t *Transaction) ReferenceExternalID() string { return t.referenceExternalID }

// ReferenceID devolve a transação interna referenciada, quando já resolvida.
func (t *Transaction) ReferenceID() (shared.ID, bool) {
	if t.referenceID == nil {
		return shared.ID{}, false
	}
	return *t.referenceID, true
}

// FailureCode devolve o código de rejeição ou falha, vazio nos demais estados.
func (t *Transaction) FailureCode() FailureCode { return t.failureCode }

// Result devolve o resultado financeiro registrado no processamento.
func (t *Transaction) Result() (Result, bool) {
	if t.result == nil {
		return Result{}, false
	}
	return *t.result, true
}

// CreatedAt devolve o instante de criação, em UTC.
func (t *Transaction) CreatedAt() time.Time { return t.createdAt }

// UpdatedAt devolve o instante da última alteração, em UTC.
func (t *Transaction) UpdatedAt() time.Time { return t.updatedAt }
