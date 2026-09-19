// Package usecase implementa os casos de uso compartilhados por HTTP e SQS.
//
// Os dois transportes chamam exatamente as mesmas funções, com os mesmos
// comandos: é o que garante que uma operação tenha o mesmo resultado
// financeiro independentemente do canal por onde chegou.
package usecase

import "github.com/vigmi/backend-challenge-go/internal/domain/shared"

// Erros de aplicação, classificáveis por `errors.Is`.
var (
	// ErrInvalidInput cobre comando malformado antes de qualquer efeito.
	ErrInvalidInput = shared.Validation("INVALID_INPUT", "requisição inválida")

	// ErrIdempotencyConflict indica chave reutilizada com conteúdo diferente.
	ErrIdempotencyConflict = shared.Conflict("IDEMPOTENCY_KEY_CONFLICT",
		"chave de idempotência já usada com outro conteúdo")

	// ErrOperationReapplied indica a mesma operação financeira reenviada com
	// outra chave de idempotência.
	ErrOperationReapplied = shared.Conflict("OPERATION_ALREADY_APPLIED",
		"operação já registrada com outra chave de idempotência")

	// ErrWalletAlreadyExists indica abertura repetida para o par (jogador, moeda).
	ErrWalletAlreadyExists = shared.Conflict("WALLET_ALREADY_EXISTS",
		"jogador já possui carteira nessa moeda")
)
