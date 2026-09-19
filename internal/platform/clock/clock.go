// Package clock fornece as implementações de tempo e identidade usadas em
// produção.
package clock

import (
	"time"

	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// System devolve o relógio do sistema, sempre em UTC.
type System struct{}

// Now devolve o instante atual em UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// UUIDGenerator cria identificadores UUIDv7.
type UUIDGenerator struct{}

// NewID gera um identificador novo.
func (UUIDGenerator) NewID() (shared.ID, error) { return shared.NewID() }

// Module expõe relógio e gerador ao grafo.
var Module = fx.Module("clock",
	fx.Provide(
		func() port.Clock { return System{} },
		func() port.IDGenerator { return UUIDGenerator{} },
	),
)
