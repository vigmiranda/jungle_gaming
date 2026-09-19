package port

import "github.com/vigmi/backend-challenge-go/internal/domain/shared"

// Erros de persistência, classificáveis por `errors.Is`.
//
// Os adapters traduzem os códigos do PostgreSQL para estes erros, de modo que
// os casos de uso não precisem conhecer SQLSTATE nem nomes de constraint.
var (
	// ErrNotFound indica registro inexistente.
	ErrNotFound = shared.Validation("NOT_FOUND", "registro não encontrado")

	// ErrConflict indica choque com um estado já persistido, como carteira
	// duplicada ou reaplicação de uma operação já registrada.
	ErrConflict = shared.Conflict("PERSISTENCE_CONFLICT", "conflito com estado já persistido")
)
