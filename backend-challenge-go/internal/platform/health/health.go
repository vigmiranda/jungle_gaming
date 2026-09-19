// Package health agrega as verificações de readiness das dependências.
package health

import "context"

// Status descreve o resultado de uma verificação.
type Status string

// Valores possíveis de Status.
const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
)

// Probe verifica uma dependência específica.
type Probe interface {
	Name() string
	Check(ctx context.Context) error
}

type probeFunc struct {
	name  string
	check func(ctx context.Context) error
}

func (p probeFunc) Name() string                    { return p.name }
func (p probeFunc) Check(ctx context.Context) error { return p.check(ctx) }

// NewProbe cria uma verificação a partir de uma função.
func NewProbe(name string, check func(ctx context.Context) error) Probe {
	return probeFunc{name: name, check: check}
}

// CheckResult é o resultado individual de uma dependência.
type CheckResult struct {
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Report é a visão consolidada devolvida pelo endpoint de readiness.
type Report struct {
	Status Status                 `json:"status"`
	Checks map[string]CheckResult `json:"checks,omitempty"`
}

// Healthy indica se todas as dependências responderam.
func (r Report) Healthy() bool {
	return r.Status == StatusOK
}

// Checker executa todas as verificações registradas.
type Checker struct {
	probes []Probe
}

// NewChecker cria o agregador com as verificações informadas.
func NewChecker(probes []Probe) *Checker {
	return &Checker{probes: probes}
}

// Check executa as verificações e consolida o resultado. Uma falha em qualquer
// dependência marca o relatório inteiro como indisponível.
func (c *Checker) Check(ctx context.Context) Report {
	report := Report{Status: StatusOK}
	if len(c.probes) == 0 {
		return report
	}

	report.Checks = make(map[string]CheckResult, len(c.probes))
	for _, probe := range c.probes {
		if err := probe.Check(ctx); err != nil {
			report.Status = StatusError
			report.Checks[probe.Name()] = CheckResult{Status: StatusError, Error: err.Error()}
			continue
		}
		report.Checks[probe.Name()] = CheckResult{Status: StatusOK}
	}
	return report
}
