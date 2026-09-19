package health

import "go.uber.org/fx"

// ProbeGroup é a tag do grupo Fx alimentado pelos adapters de infraestrutura.
const ProbeGroup = `group:"health_probes"`

// Module monta o Checker a partir de todas as verificações registradas no grupo.
var Module = fx.Module("health",
	fx.Provide(
		fx.Annotate(NewChecker, fx.ParamTags(ProbeGroup)),
	),
)
