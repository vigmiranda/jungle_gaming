// Package shared reúne os tipos de erro comuns ao domínio.
//
// Erros de domínio são classificáveis por `errors.Is` e carregam um código
// estável, que os adapters traduzem para o `failureCode` do contrato externo.
// Rejeições de negócio nunca usam `panic`.
package shared

import "fmt"

// Kind classifica o erro de domínio, permitindo que a borda escolha o status
// HTTP sem inspecionar mensagens.
type Kind string

// Classes de erro do domínio.
const (
	// KindValidation indica entrada malformada ou fora do contrato.
	KindValidation Kind = "VALIDATION"
	// KindRejection indica recusa por regra de negócio, com resultado terminal.
	KindRejection Kind = "REJECTION"
	// KindConflict indica choque com um estado já persistido.
	KindConflict Kind = "CONFLICT"
	// KindInvariant indica uso indevido do domínio, como transição ilegal.
	KindInvariant Kind = "INVARIANT"
)

// Error é o erro de domínio, identificado por um código estável.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap expõe a causa para `errors.Is` e `errors.As`.
func (e *Error) Unwrap() error { return e.cause }

// Is compara erros de domínio pelo código, ignorando a mensagem formatada.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == other.Code
}

// WithCause devolve uma cópia encadeando a causa de origem.
func (e *Error) WithCause(cause error) *Error {
	clone := *e
	clone.cause = cause
	return &clone
}

// Messagef devolve uma cópia com a mensagem detalhada, preservando o código.
func (e *Error) Messagef(format string, args ...any) *Error {
	clone := *e
	clone.Message = fmt.Sprintf(format, args...)
	return &clone
}

func newError(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

// Validation cria um erro de entrada inválida.
func Validation(code, message string) *Error { return newError(KindValidation, code, message) }

// Rejection cria uma recusa por regra de negócio.
func Rejection(code, message string) *Error { return newError(KindRejection, code, message) }

// Conflict cria um erro de conflito com estado persistido.
func Conflict(code, message string) *Error { return newError(KindConflict, code, message) }

// Invariant cria um erro de uso indevido do domínio.
func Invariant(code, message string) *Error { return newError(KindInvariant, code, message) }
