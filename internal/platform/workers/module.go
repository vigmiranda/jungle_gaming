package workers

import "go.uber.org/fx"

// Module registra o worker de referências pendentes.
var Module = fx.Module("workers",
	fx.Provide(NewPendingReferenceWorker),
	fx.Invoke(RegisterPendingReference),
)
