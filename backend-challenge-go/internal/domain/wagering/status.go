package wagering

import "github.com/vigmi/backend-challenge-go/internal/domain/shared"

// Status é o estado da transação.
type Status string

// Estados possíveis.
const (
	// Pending indica registro aceito com processamento não concluído.
	Pending Status = "PENDING"
	// PendingReference indica espera por uma referência ainda indisponível.
	PendingReference Status = "PENDING_REFERENCE"
	// Processed indica conclusão bem-sucedida. Terminal.
	Processed Status = "PROCESSED"
	// Rejected indica recusa por regra de negócio. Terminal.
	Rejected Status = "REJECTED"
	// Failed indica falha permanente de infraestrutura, registrada para
	// auditoria. Terminal.
	Failed Status = "FAILED"
)

// Erros da máquina de estados.
var (
	// ErrUnknownStatus cobre estados fora da lista suportada.
	ErrUnknownStatus = shared.Validation("UNKNOWN_STATUS", "estado de transação desconhecido")
	// ErrIllegalTransition cobre transições não permitidas, inclusive a partir
	// de um estado terminal.
	ErrIllegalTransition = shared.Invariant("ILLEGAL_TRANSITION", "transição de estado não permitida")
)

// allowedTransitions declara a máquina de estados.
//
// Estados terminais não aparecem como origem: uma transação concluída nunca
// volta a transitar, e o replay apenas lê o resultado persistido.
var allowedTransitions = map[Status][]Status{
	Pending:          {PendingReference, Processed, Rejected, Failed},
	PendingReference: {Processed, Rejected, Failed},
}

// ParseStatus converte o estado persistido.
func ParseStatus(raw string) (Status, error) {
	status := Status(raw)
	switch status {
	case Pending, PendingReference, Processed, Rejected, Failed:
		return status, nil
	default:
		return "", ErrUnknownStatus.Messagef("estado %q não é suportado", raw)
	}
}

// String devolve a representação textual.
func (s Status) String() string { return string(s) }

// IsTerminal indica estados que não admitem nova transição.
func (s Status) IsTerminal() bool {
	return s == Processed || s == Rejected || s == Failed
}

// CanTransitionTo indica se a transição é permitida.
func (s Status) CanTransitionTo(target Status) bool {
	for _, allowed := range allowedTransitions[s] {
		if allowed == target {
			return true
		}
	}
	return false
}
