// Package app compõe a aplicação a partir dos módulos Fx.
//
// O domínio permanece fora daqui: apenas a borda conhece Fx.
package app

import (
	"log/slog"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/awssqs"
	"github.com/vigmi/backend-challenge-go/internal/platform/health"
	"github.com/vigmi/backend-challenge-go/internal/platform/httpserver"
	"github.com/vigmi/backend-challenge-go/internal/platform/logging"
	"github.com/vigmi/backend-challenge-go/internal/platform/postgres"
)

// Module reúne todos os módulos da aplicação.
//
// A ordem de declaração não define a ordem de start: o Fx resolve as
// dependências pelo grafo e desliga os hooks na ordem inversa, garantindo que
// as conexões fechem depois dos componentes que as usam.
func Module() fx.Option {
	return fx.Options(
		config.Module,
		logging.Module,
		health.Module,
		postgres.Module,
		awssqs.Module,
		httpserver.Module,
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: log}
		}),
	)
}
