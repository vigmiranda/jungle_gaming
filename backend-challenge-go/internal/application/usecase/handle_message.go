package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// Erros de envelope e origem do canal SQS.
var (
	ErrInvalidEnvelope      = shared.Validation("INVALID_ENVELOPE", "envelope SQS inválido")
	ErrUnauthorizedProvider = shared.Validation("UNAUTHORIZED_PROVIDER",
		"providerId não autorizado neste canal")
	ErrInboxPayloadMismatch = shared.Conflict("INBOX_PAYLOAD_MISMATCH",
		"mesma mensagem reentregue com payload diferente")
)

const wagerRequestedType = "WagerTransactionRequested"

// WagerMessageEnvelope é o contrato de entrada da fila wager-transactions.
type WagerMessageEnvelope struct {
	MessageID  string          `json:"messageId"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurredAt"`
	Data       json.RawMessage `json:"data"`
}

type wagerMessageData struct {
	ProviderID                     string     `json:"providerId"`
	ExternalTransactionID          string     `json:"externalTransactionId"`
	IdempotencyKey                 string     `json:"idempotencyKey"`
	PlayerID                       string     `json:"playerId"`
	WalletID                       string     `json:"walletId"`
	RoundID                        string     `json:"roundId"`
	GameID                         string     `json:"gameId"`
	Kind                           string     `json:"kind"`
	Money                          MoneyInput `json:"money"`
	ReferenceExternalTransactionID string     `json:"referenceExternalTransactionId"`
}

// HandleWagerMessage aplica uma entrega SQS com inbox na mesma UoW do domínio.
type HandleWagerMessage struct {
	unitOfWork       port.UnitOfWork
	process          *ProcessWagerTransaction
	clock            port.Clock
	ids              port.IDGenerator
	consumerName     string
	allowedProviders map[string]struct{}
}

// NewHandleWagerMessage monta o caso de uso do consumidor.
func NewHandleWagerMessage(
	unitOfWork port.UnitOfWork,
	process *ProcessWagerTransaction,
	clock port.Clock,
	ids port.IDGenerator,
	consumerName string,
	allowedProviders []string,
) *HandleWagerMessage {
	allowed := make(map[string]struct{}, len(allowedProviders))
	for _, provider := range allowedProviders {
		if provider != "" {
			allowed[provider] = struct{}{}
		}
	}
	return &HandleWagerMessage{
		unitOfWork:       unitOfWork,
		process:          process,
		clock:            clock,
		ids:              ids,
		consumerName:     consumerName,
		allowedProviders: allowed,
	}
}

// Handle processa o envelope. Erros transitórios devem provocar retry; erros
// de envelope/origem são terminais para o worker (DLQ).
func (uc *HandleWagerMessage) Handle(ctx context.Context, raw []byte) (TransactionResult, error) {
	command, messageID, hash, err := uc.parseEnvelope(raw)
	if err != nil {
		return TransactionResult{}, err
	}

	var result TransactionResult
	err = uc.unitOfWork.Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		existing, err := repositories.Inbox().Find(ctx, uc.consumerName, messageID)
		if err == nil {
			if existing.PayloadHash != hash {
				return ErrInboxPayloadMismatch
			}
			// Reentrega após commit: sem segundo efeito financeiro.
			return nil
		}
		if !errors.Is(err, port.ErrNotFound) {
			return err
		}

		inboxID, err := uc.ids.NewID()
		if err != nil {
			return err
		}

		err = repositories.Inbox().Record(ctx, port.InboxMessage{
			ID:           inboxID,
			ConsumerName: uc.consumerName,
			MessageID:    messageID,
			PayloadHash:  hash,
			ReceivedAt:   uc.clock.Now(),
		})
		if err != nil {
			if !errors.Is(err, port.ErrConflict) {
				return err
			}
			// Corrida com outro worker: a outra entrega venceu.
			existing, findErr := repositories.Inbox().Find(ctx, uc.consumerName, messageID)
			if findErr != nil {
				return findErr
			}
			if existing.PayloadHash != hash {
				return ErrInboxPayloadMismatch
			}
			return nil
		}

		var procErr error
		result, procErr = uc.process.ExecuteIn(ctx, repositories, command)
		if procErr == nil {
			return nil
		}
		if isTransientMessageError(procErr) {
			return procErr
		}
		// Conflito ou entrada inválida permanente: a inbox fica gravada para
		// que a reentrega não repita o trabalho; o worker remove a mensagem.
		return nil
	})
	if err != nil {
		return TransactionResult{}, err
	}
	return result, nil
}

func (uc *HandleWagerMessage) parseEnvelope(raw []byte) (ProcessTransactionCommand, string, string, error) {
	var envelope WagerMessageEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ProcessTransactionCommand{}, "", "", ErrInvalidEnvelope.WithCause(err)
	}
	if envelope.MessageID == "" || envelope.Type != wagerRequestedType || len(envelope.Data) == 0 {
		return ProcessTransactionCommand{}, "", "", ErrInvalidEnvelope
	}

	var data wagerMessageData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return ProcessTransactionCommand{}, "", "", ErrInvalidEnvelope.WithCause(err)
	}
	if _, ok := uc.allowedProviders[data.ProviderID]; !ok {
		return ProcessTransactionCommand{}, "", "", ErrUnauthorizedProvider.
			Messagef("providerId %q não está na política do consumidor", data.ProviderID)
	}

	command := ProcessTransactionCommand{
		IdempotencyKey:                 data.IdempotencyKey,
		ProviderID:                     data.ProviderID,
		ExternalTransactionID:          data.ExternalTransactionID,
		PlayerID:                       data.PlayerID,
		WalletID:                       data.WalletID,
		RoundID:                        data.RoundID,
		GameID:                         data.GameID,
		Kind:                           data.Kind,
		Money:                          data.Money,
		ReferenceExternalTransactionID: data.ReferenceExternalTransactionID,
	}
	return command, envelope.MessageID, payloadHash(command), nil
}

func isTransientMessageError(err error) bool {
	var domainErr *shared.Error
	if errors.As(err, &domainErr) {
		switch domainErr.Kind {
		case shared.KindValidation, shared.KindConflict, shared.KindRejection, shared.KindInvariant:
			return false
		}
	}
	return true
}
