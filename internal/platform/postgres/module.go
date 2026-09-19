package postgres

import (
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

// Module provê o pool e registra a verificação de readiness no grupo de probes.
var Module = fx.Module("postgres",
	fx.Provide(
		NewPool,
		fx.Annotate(NewProbe, fx.ResultTags(health.ProbeGroup)),
	),
)
