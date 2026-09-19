package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/platform/metrics"
)

func TestMetricsExposePrometheusText(t *testing.T) {
	m := metrics.New()
	m.RecordTransaction("http", "PROCESSED", false)
	m.RecordTransaction("sqs", "REJECTED", true)
	m.RecordRetry("outbox")
	m.RecordDLQ()
	m.RecordConflict("http")
	m.RecordOutboxLag(1.5)
	m.RecordReconciliationDivergence()
	m.RecordProcessingDuration("http", 0.02)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, fragment := range []string{
		"wagering_transactions_total",
		`channel="http"`,
		`status="PROCESSED"`,
		"wagering_idempotent_replays_total",
		"wagering_retries_total",
		"wagering_dlq_messages_total",
		"wagering_concurrency_conflicts_total",
		"wagering_outbox_lag_seconds",
		"wagering_reconciliation_divergences_total",
		"wagering_processing_duration_seconds",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("métricas não contêm %q\n%s", fragment, text)
		}
	}
}

func TestNilMetricsAreNoop(t *testing.T) {
	var m *metrics.Metrics
	m.RecordTransaction("http", "PROCESSED", true)
	m.RecordRetry("sqs")
	m.RecordDLQ()
	m.RecordConflict("sqs")
	m.RecordOutboxLag(-1)
	m.RecordReconciliationDivergence()
	m.RecordProcessingDuration("sqs", -1)
}
