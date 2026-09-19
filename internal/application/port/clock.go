package port

import (
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// Clock fornece o instante atual.
//
// O tempo entra por injeção para que os casos de uso sejam determinísticos nos
// testes e para que o instante gravado em transação, ledger e evento seja o
// mesmo dentro de uma operação.
type Clock interface {
	Now() time.Time
}

// IDGenerator cria identificadores internos.
type IDGenerator interface {
	NewID() (shared.ID, error)
}
