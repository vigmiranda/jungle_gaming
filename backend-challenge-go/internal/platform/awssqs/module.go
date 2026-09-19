package awssqs

import (
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

// Module provê o cliente SQS, o consumidor e a verificação de readiness.
var Module = fx.Module("sqs",
	fx.Provide(
		NewClient,
		NewConsumer,
		fx.Annotate(NewProbe, fx.ResultTags(health.ProbeGroup)),
	),
	fx.Invoke(RegisterConsumer),
)
