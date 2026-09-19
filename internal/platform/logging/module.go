package logging

import "go.uber.org/fx"

// Module expõe o logger estruturado ao grafo.
var Module = fx.Module("logging", fx.Provide(New))
