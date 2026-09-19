// Command api sobe a API HTTP e os workers do serviço de apostas.
package main

import (
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/app"
)

func main() {
	fx.New(app.Module()).Run()
}
