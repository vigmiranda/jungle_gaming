package httpserver

import "go.uber.org/fx"

// Module monta o servidor HTTP e o registra no ciclo de vida.
var Module = fx.Module("http",
	fx.Provide(
		NewHealthHandler,
		NewRouter,
		NewServer,
	),
	fx.Invoke(Register),
)
