package usecase

import (
	"testing"
	"time"
)

func TestNewResolvePendingReferencesAppliesDefaults(t *testing.T) {
	resolve := NewResolvePendingReferences(nil, nil, nil, PendingReferencePolicy{})
	if resolve.policy.MaxAttempts != 10 || resolve.policy.TTL != 5*time.Minute {
		t.Fatalf("defaults = %+v", resolve.policy)
	}
	if resolve.policy.BackoffBase != time.Second || resolve.policy.BackoffMax != 30*time.Second {
		t.Fatalf("backoff = %+v", resolve.policy)
	}
	if resolve.policy.BatchSize != 10 {
		t.Fatalf("batch = %d", resolve.policy.BatchSize)
	}
}

func TestReferenceBackoff(t *testing.T) {
	if referenceBackoff(1, time.Second, time.Minute) != time.Second {
		t.Fatal("base")
	}
	if referenceBackoff(0, time.Second, time.Minute) != time.Second {
		t.Fatal("zero attempts")
	}
	if referenceBackoff(10, time.Second, 8*time.Second) != 8*time.Second {
		t.Fatal("cap")
	}
	if referenceBackoff(2, 4*time.Second, 8*time.Second) != 8*time.Second {
		t.Fatal("half-max short-circuit")
	}
}
