package usecase

import (
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
)

type captureRecorder struct {
	transactions []string
	retries      []string
}

func (r *captureRecorder) RecordTransaction(channel, status string, _ bool) {
	r.transactions = append(r.transactions, channel+":"+status)
}
func (r *captureRecorder) RecordRetry(component string)             { r.retries = append(r.retries, component) }
func (r *captureRecorder) RecordDLQ()                               {}
func (r *captureRecorder) RecordConflict(string)                    {}
func (r *captureRecorder) RecordOutboxLag(float64)                  {}
func (r *captureRecorder) RecordReconciliationDivergence()          {}
func (r *captureRecorder) RecordProcessingDuration(string, float64) {}

func TestObservePendingRecordsRetryAndTerminal(t *testing.T) {
	rec := &captureRecorder{}
	uc := &ResolvePendingReferences{recorder: rec}

	uc.observePending(TransactionResult{})
	uc.observePending(TransactionResult{Status: wagering.PendingReference})
	uc.observePending(TransactionResult{Status: wagering.Processed})

	if len(rec.retries) != 1 || rec.retries[0] != "pending_reference" {
		t.Fatalf("retries = %#v", rec.retries)
	}
	if len(rec.transactions) != 1 || rec.transactions[0] != "pending_reference:PROCESSED" {
		t.Fatalf("transactions = %#v", rec.transactions)
	}
}
