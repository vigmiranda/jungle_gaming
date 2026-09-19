package wagering_test

import (
	"errors"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

func TestParseStatusAcceptsKnownStates(t *testing.T) {
	for _, raw := range []string{"PENDING", "PENDING_REFERENCE", "PROCESSED", "REJECTED", "FAILED"} {
		t.Run(raw, func(t *testing.T) {
			parsed, err := wagering.ParseStatus(raw)
			if err != nil {
				t.Fatalf("ParseStatus devolveu erro: %v", err)
			}
			if parsed.String() != raw {
				t.Errorf("String = %q, esperado %q", parsed.String(), raw)
			}
		})
	}
}

func TestParseStatusRejectsUnknownStates(t *testing.T) {
	for _, raw := range []string{"", "pending", "CANCELLED"} {
		t.Run(raw, func(t *testing.T) {
			_, err := wagering.ParseStatus(raw)

			if !errors.Is(err, wagering.ErrUnknownStatus) {
				t.Errorf("erro = %v, esperado ErrUnknownStatus", err)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		status   wagering.Status
		terminal bool
	}{
		{wagering.Pending, false},
		{wagering.PendingReference, false},
		{wagering.Processed, true},
		{wagering.Rejected, true},
		{wagering.Failed, true},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			if tt.status.IsTerminal() != tt.terminal {
				t.Errorf("IsTerminal = %v, esperado %v", tt.status.IsTerminal(), tt.terminal)
			}
		})
	}
}

// Estados terminais não admitem nova transição: o replay lê o resultado
// persistido em vez de reaplicar a operação.
func TestCanTransitionToCoversTheWholeMachine(t *testing.T) {
	all := []wagering.Status{
		wagering.Pending, wagering.PendingReference,
		wagering.Processed, wagering.Rejected, wagering.Failed,
	}
	allowed := map[wagering.Status]map[wagering.Status]bool{
		wagering.Pending: {
			wagering.PendingReference: true,
			wagering.Processed:        true,
			wagering.Rejected:         true,
			wagering.Failed:           true,
		},
		wagering.PendingReference: {
			wagering.Processed: true,
			wagering.Rejected:  true,
			wagering.Failed:    true,
		},
	}

	for _, from := range all {
		for _, to := range all {
			want := allowed[from][to]

			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("CanTransitionTo(%s → %s) = %v, esperado %v", from, to, got, want)
			}
		}
	}
}
