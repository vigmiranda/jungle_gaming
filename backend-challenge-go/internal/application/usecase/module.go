package usecase

import "go.uber.org/fx"

// Module expõe os casos de uso ao grafo.
//
// HTTP e SQS consomem exatamente estas instâncias: um único fluxo de aplicação
// para os dois transportes.
var Module = fx.Module("usecase",
	fx.Provide(
		NewOpenWallet,
		NewProcessWagerTransaction,
		NewReconcileWallet,
	),
)
