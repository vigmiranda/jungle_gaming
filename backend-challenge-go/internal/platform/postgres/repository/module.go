package repository

import (
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
)

// Module expõe a unidade de trabalho ao grafo, já satisfazendo o port.
//
// Os casos de uso dependem da interface; a implementação `pgx` fica confinada
// à borda de infraestrutura.
var Module = fx.Module("repository",
	fx.Provide(
		NewUnitOfWork,
		func(unitOfWork *UnitOfWork) port.UnitOfWork { return unitOfWork },
	),
)
