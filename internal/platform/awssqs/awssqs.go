// Package awssqs provê o cliente SQS (LocalStack em ambiente local) e sua
// verificação de readiness.
package awssqs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

// NewClient monta o cliente SQS apontando para o endpoint configurado.
func NewClient(cfg config.Config) (*sqs.Client, error) {
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.SQS.Region),
	}
	if cfg.SQS.AccessKeyID != "" {
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.SQS.AccessKeyID, cfg.SQS.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, fmt.Errorf("sqs: não foi possível carregar a configuração AWS: %w", err)
	}

	return sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.SQS.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.SQS.Endpoint)
		}
	}), nil
}

// QueueInspector expõe apenas o que o probe precisa do cliente SQS.
type QueueInspector interface {
	GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
}

// NewProbe verifica se a fila de entrada está acessível.
func NewProbe(client *sqs.Client, cfg config.Config) health.Probe {
	return newProbe(client, cfg.SQS.WagerQueueURL)
}

func newProbe(client QueueInspector, queueURL string) health.Probe {
	return health.NewProbe("sqs", func(ctx context.Context) error {
		_, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(queueURL),
			AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
		})
		if err != nil {
			return fmt.Errorf("sqs: fila inacessível: %w", err)
		}
		return nil
	})
}
