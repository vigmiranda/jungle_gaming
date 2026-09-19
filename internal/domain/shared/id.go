package shared

import "github.com/google/uuid"

// ErrInvalidID cobre identificadores malformados ou não inicializados.
var ErrInvalidID = Validation("INVALID_ID", "identificador inválido")

// ID é o identificador interno das entidades do domínio.
//
// Usa UUIDv7, que é ordenável por tempo de criação: isso mantém a inserção no
// índice localizada e dá ordenação natural às leituras por identificador.
type ID struct {
	value uuid.UUID
}

// NewID gera um identificador novo.
func NewID() (ID, error) {
	generated, err := uuid.NewV7()
	if err != nil {
		return ID{}, ErrInvalidID.Messagef("falha ao gerar identificador").WithCause(err)
	}
	return ID{value: generated}, nil
}

// ParseID converte a representação textual, rejeitando o UUID nulo.
func ParseID(raw string) (ID, error) {
	if raw == "" {
		return ID{}, ErrInvalidID.Messagef("identificador vazio")
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return ID{}, ErrInvalidID.Messagef("identificador malformado: %q", raw).WithCause(err)
	}
	if parsed == uuid.Nil {
		return ID{}, ErrInvalidID.Messagef("identificador nulo não é aceito")
	}
	return ID{value: parsed}, nil
}

// String devolve a representação textual, vazia quando não inicializado.
func (id ID) String() string {
	if id.IsZero() {
		return ""
	}
	return id.value.String()
}

// IsZero indica um identificador não inicializado.
func (id ID) IsZero() bool { return id.value == uuid.Nil }

// Validate rejeita o identificador não inicializado.
func (id ID) Validate() error {
	if id.IsZero() {
		return ErrInvalidID.Messagef("identificador não inicializado")
	}
	return nil
}

// Equal compara dois identificadores.
func (id ID) Equal(other ID) bool { return id.value == other.value }
