package usecase

import (
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/config"
)

// Module expõe os casos de uso ao grafo.
//
// HTTP e SQS consomem exatamente estas instâncias: um único fluxo de aplicação
// para os dois transportes.
var Module = fx.Module("usecase",
	fx.Provide(
		NewOpenWallet,
		NewProcessWagerTransaction,
		NewReconcileWallet,
		NewHandleWagerMessageFromConfig,
		NewResolvePendingReferencesFromConfig,
	),
)

// NewHandleWagerMessageFromConfig adapta a configuração ao caso de uso.
func NewHandleWagerMessageFromConfig(
	unitOfWork port.UnitOfWork,
	process *ProcessWagerTransaction,
	clock port.Clock,
	ids port.IDGenerator,
	cfg config.Config,
) *HandleWagerMessage {
	return NewHandleWagerMessage(
		unitOfWork,
		process,
		clock,
		ids,
		cfg.SQS.ConsumerName,
		cfg.SQS.AllowedProviders,
	)
}

// NewResolvePendingReferencesFromConfig adapta a política configurável.
func NewResolvePendingReferencesFromConfig(
	unitOfWork port.UnitOfWork,
	clock port.Clock,
	process *ProcessWagerTransaction,
	cfg config.Config,
) *ResolvePendingReferences {
	return NewResolvePendingReferences(unitOfWork, clock, process, PendingReferencePolicy{
		MaxAttempts: cfg.PendingReference.MaxAttempts,
		TTL:         cfg.PendingReference.TTL,
		BackoffBase: cfg.PendingReference.BackoffBase,
		BackoffMax:  cfg.PendingReference.BackoffMax,
		BatchSize:   cfg.PendingReference.BatchSize,
	})
}
