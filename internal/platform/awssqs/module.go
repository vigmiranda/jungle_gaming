package awssqs

import (
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

// Module provê o cliente SQS, o consumidor, o publisher e a verificação de readiness.
var Module = fx.Module("sqs",
	fx.Provide(
		NewClient,
		NewConsumer,
		NewPublisher,
		fx.Annotate(NewProbe, fx.ResultTags(health.ProbeGroup)),
	),
	fx.Invoke(RegisterConsumer, RegisterPublisher),
)
