package awssqs

import (
	"testing"
)

func TestCorrelationFromEnvelopePrefersCorrelationID(t *testing.T) {
	id := correlationFromEnvelope(`{"messageId":"msg-1","correlationId":"corr-9","type":"WagerTransactionRequested","data":{}}`)
	if id != "corr-9" {
		t.Fatalf("got %q", id)
	}
}

func TestCorrelationFromEnvelopeFallsBackToMessageID(t *testing.T) {
	id := correlationFromEnvelope(`{"messageId":"msg-1","type":"WagerTransactionRequested","data":{}}`)
	if id != "msg-1" {
		t.Fatalf("got %q", id)
	}
}

func TestCorrelationFromEnvelopeGeneratesWhenAbsent(t *testing.T) {
	id := correlationFromEnvelope(`not-json`)
	if id == "" {
		t.Fatal("esperava UUID gerado")
	}
}
