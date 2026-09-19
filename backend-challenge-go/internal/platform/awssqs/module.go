package awssqs

import (
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

// Module provê o cliente SQS e registra a verificação de readiness no grupo de probes.
var Module = fx.Module("sqs",
	fx.Provide(
		NewClient,
		fx.Annotate(NewProbe, fx.ResultTags(health.ProbeGroup)),
	),
)
