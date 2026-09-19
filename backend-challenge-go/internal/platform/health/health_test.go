package health_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

func TestCheckerWithoutProbesIsHealthy(t *testing.T) {
	report := health.NewChecker(nil).Check(context.Background())

	if !report.Healthy() {
		t.Errorf("relatório = %+v, esperado saudável", report)
	}
	if report.Checks != nil {
		t.Errorf("Checks = %+v, esperado nil sem verificações", report.Checks)
	}
}

func TestCheckerReportsEveryProbe(t *testing.T) {
	failure := errors.New("conexão recusada")
	checker := health.NewChecker([]health.Probe{
		health.NewProbe("postgres", func(context.Context) error { return nil }),
		health.NewProbe("sqs", func(context.Context) error { return failure }),
	})

	report := checker.Check(context.Background())

	if report.Healthy() {
		t.Error("relatório deveria estar indisponível com uma dependência em falha")
	}
	if report.Status != health.StatusError {
		t.Errorf("Status = %q, esperado %q", report.Status, health.StatusError)
	}
	if got := report.Checks["postgres"]; got.Status != health.StatusOK || got.Error != "" {
		t.Errorf("postgres = %+v, esperado ok sem erro", got)
	}
	if got := report.Checks["sqs"]; got.Status != health.StatusError || got.Error != failure.Error() {
		t.Errorf("sqs = %+v, esperado erro %q", got, failure)
	}
}

func TestCheckerHealthyWhenAllProbesPass(t *testing.T) {
	checker := health.NewChecker([]health.Probe{
		health.NewProbe("postgres", func(context.Context) error { return nil }),
	})

	report := checker.Check(context.Background())

	if !report.Healthy() {
		t.Errorf("relatório = %+v, esperado saudável", report)
	}
	if len(report.Checks) != 1 {
		t.Errorf("Checks = %+v, esperado uma entrada", report.Checks)
	}
}

func TestProbeCarriesNameAndPropagatesContext(t *testing.T) {
	type ctxKey struct{}
	var seen bool

	probe := health.NewProbe("postgres", func(ctx context.Context) error {
		seen = ctx.Value(ctxKey{}) == "presente"
		return nil
	})

	if probe.Name() != "postgres" {
		t.Errorf("Name = %q, esperado \"postgres\"", probe.Name())
	}
	if err := probe.Check(context.WithValue(context.Background(), ctxKey{}, "presente")); err != nil {
		t.Fatalf("Check devolveu erro: %v", err)
	}
	if !seen {
		t.Error("Check não recebeu o contexto original")
	}
}
