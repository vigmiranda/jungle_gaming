package awssqs

import (
	"errors"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
)

func TestIsPoisonClassifiesTerminalEnvelopeErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		poison bool
	}{
		{"envelope", usecase.ErrInvalidEnvelope, true},
		{"provider", usecase.ErrUnauthorizedProvider, true},
		{"inbox mismatch", usecase.ErrInboxPayloadMismatch, true},
		{"wrapped", errors.Join(errors.New("outer"), usecase.ErrInvalidEnvelope), true},
		{"transient", errors.New("timeout"), false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPoison(tt.err); got != tt.poison {
				t.Fatalf("isPoison(%v) = %v, esperado %v", tt.err, got, tt.poison)
			}
		})
	}
}
