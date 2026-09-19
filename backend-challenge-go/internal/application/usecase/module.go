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
