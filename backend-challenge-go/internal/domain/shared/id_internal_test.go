package shared

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("fonte de entropia indisponível")
}

// A geração só falha quando a fonte de entropia falha. O teste substitui essa
// fonte para provar que a falha vira erro de domínio, sem panic.
func TestNewIDFailsWhenEntropySourceFails(t *testing.T) {
	uuid.SetRand(failingReader{})
	t.Cleanup(func() { uuid.SetRand(nil) })

	_, err := NewID()

	if !errors.Is(err, ErrInvalidID) {
		t.Errorf("erro = %v, esperado ErrInvalidID", err)
	}
	if errors.Unwrap(err) == nil {
		t.Error("o erro deveria encadear a causa original")
	}
}
