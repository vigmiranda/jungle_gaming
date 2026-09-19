package metrics

import (
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
)

// Module expõe o registry Prometheus ao grafo.
var Module = fx.Module("metrics",
	fx.Provide(
		New,
		func(m *Metrics) port.Recorder { return m },
	),
)
