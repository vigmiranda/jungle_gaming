package awssqs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/vigmi/backend-challenge-go/internal/config"
)

type stubInspector struct {
	queueURL string
	err      error
}

func (s *stubInspector) GetQueueAttributes(_ context.Context, params *sqs.GetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	s.queueURL = *params.QueueUrl
	if s.err != nil {
		return nil, s.err
	}
	return &sqs.GetQueueAttributesOutput{}, nil
}

func TestProbeQueriesConfiguredQueue(t *testing.T) {
	inspector := &stubInspector{}
	probe := newProbe(inspector, "http://localhost:4566/000000000000/wager-transactions.fifo")

	if probe.Name() != "sqs" {
		t.Errorf("Name = %q, esperado \"sqs\"", probe.Name())
	}
	if err := probe.Check(context.Background()); err != nil {
		t.Fatalf("Check devolveu erro: %v", err)
	}
	if inspector.queueURL != "http://localhost:4566/000000000000/wager-transactions.fifo" {
		t.Errorf("fila consultada = %q", inspector.queueURL)
	}
}

func TestProbeWrapsFailure(t *testing.T) {
	failure := errors.New("connection refused")
	probe := newProbe(&stubInspector{err: failure}, "http://localhost:4566/queue")

	err := probe.Check(context.Background())

	if err == nil {
		t.Fatal("esperava erro quando a fila está inacessível")
	}
	if !errors.Is(err, failure) {
		t.Errorf("erro não encapsula a causa original: %v", err)
	}
	if !strings.Contains(err.Error(), "fila inacessível") {
		t.Errorf("mensagem inesperada: %v", err)
	}
}

func TestNewClientUsesEndpointAndStaticCredentials(t *testing.T) {
	client, err := NewClient(config.Config{SQS: config.SQS{
		Region:          "sa-east-1",
		Endpoint:        "http://localhost:4566",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		WagerQueueURL:   "http://localhost:4566/000000000000/wager-transactions.fifo",
	}})
	if err != nil {
		t.Fatalf("NewClient devolveu erro: %v", err)
	}

	options := client.Options()
	if options.Region != "sa-east-1" {
		t.Errorf("Region = %q, esperado \"sa-east-1\"", options.Region)
	}
	if options.BaseEndpoint == nil || *options.BaseEndpoint != "http://localhost:4566" {
		t.Errorf("BaseEndpoint = %v, esperado o endpoint do LocalStack", options.BaseEndpoint)
	}

	creds, err := options.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve devolveu erro: %v", err)
	}
	if creds.AccessKeyID != "test" {
		t.Errorf("AccessKeyID = %q, esperado \"test\"", creds.AccessKeyID)
	}
}

func TestNewClientWithoutEndpointKeepsDefault(t *testing.T) {
	client, err := NewClient(config.Config{SQS: config.SQS{Region: "us-east-1"}})
	if err != nil {
		t.Fatalf("NewClient devolveu erro: %v", err)
	}

	if endpoint := client.Options().BaseEndpoint; endpoint != nil {
		t.Errorf("BaseEndpoint = %q, esperado nil sem AWS_ENDPOINT_URL", *endpoint)
	}
}

func TestNewProbeUsesWagerQueueFromConfig(t *testing.T) {
	probe := NewProbe(&sqs.Client{}, config.Config{SQS: config.SQS{
		WagerQueueURL: "http://localhost:4566/000000000000/wager-transactions.fifo",
	}})

	if probe.Name() != "sqs" {
		t.Errorf("Name = %q, esperado \"sqs\"", probe.Name())
	}
}
