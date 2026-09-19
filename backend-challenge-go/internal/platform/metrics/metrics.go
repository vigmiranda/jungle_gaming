// Package metrics expõe contadores e histogramas Prometheus da aplicação.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
)

// Metrics implementa port.Recorder com um registry próprio (seguro em testes).
type Metrics struct {
	registry *prometheus.Registry

	transactions   *prometheus.CounterVec
	replays        *prometheus.CounterVec
	retries        *prometheus.CounterVec
	dlq            prometheus.Counter
	conflicts      *prometheus.CounterVec
	outboxLag      prometheus.Histogram
	reconciliation prometheus.Counter
	duration       *prometheus.HistogramVec
}

// New monta o conjunto de métricas com registry isolado.
func New() *Metrics {
	registry := prometheus.NewRegistry()
	m := &Metrics{
		registry: registry,
		transactions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wagering_transactions_total",
			Help: "Resultados de operações de wagering por status e canal",
		}, []string{"channel", "status"}),
		replays: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wagering_idempotent_replays_total",
			Help: "Replays idempotentes por canal",
		}, []string{"channel"}),
		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wagering_retries_total",
			Help: "Retries de SQS, outbox e pending-reference",
		}, []string{"component"}),
		dlq: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wagering_dlq_messages_total",
			Help: "Mensagens movidas para a DLQ",
		}),
		conflicts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wagering_concurrency_conflicts_total",
			Help: "Conflitos de concorrência / unicidade por origem",
		}, []string{"source"}),
		outboxLag: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "wagering_outbox_lag_seconds",
			Help:    "Atraso entre occurredAt do evento e a publicação",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		}),
		reconciliation: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wagering_reconciliation_divergences_total",
			Help: "Divergências detectadas na reconciliação de carteira",
		}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "wagering_processing_duration_seconds",
			Help:    "Latência de processamento por canal",
			Buckets: prometheus.DefBuckets,
		}, []string{"channel"}),
	}

	registry.MustRegister(
		m.transactions,
		m.replays,
		m.retries,
		m.dlq,
		m.conflicts,
		m.outboxLag,
		m.reconciliation,
		m.duration,
	)
	// Séries canônicas em zero: o scrape inicial expõe os nomes (Bruno/ops)
	// antes do primeiro evento de negócio.
	m.transactions.WithLabelValues("http", "PROCESSED")
	m.replays.WithLabelValues("http")
	m.retries.WithLabelValues("sqs")
	m.conflicts.WithLabelValues("http")
	m.duration.WithLabelValues("http")
	return m
}

// Handler expõe o scrape Prometheus em texto.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry})
}

// RecordTransaction incrementa o contador de status (e replay, se houver).
func (m *Metrics) RecordTransaction(channel, status string, replay bool) {
	if m == nil {
		return
	}
	if status == "" {
		status = "UNKNOWN"
	}
	m.transactions.WithLabelValues(channel, status).Inc()
	if replay {
		m.replays.WithLabelValues(channel).Inc()
	}
}

// RecordRetry incrementa retries do componente (sqs|outbox|pending_reference).
func (m *Metrics) RecordRetry(component string) {
	if m == nil {
		return
	}
	m.retries.WithLabelValues(component).Inc()
}

// RecordDLQ incrementa mensagens enviadas à DLQ.
func (m *Metrics) RecordDLQ() {
	if m == nil {
		return
	}
	m.dlq.Inc()
}

// RecordConflict incrementa conflitos de concorrência.
func (m *Metrics) RecordConflict(source string) {
	if m == nil {
		return
	}
	m.conflicts.WithLabelValues(source).Inc()
}

// RecordOutboxLag observa o atraso de publicação.
func (m *Metrics) RecordOutboxLag(seconds float64) {
	if m == nil {
		return
	}
	if seconds < 0 {
		seconds = 0
	}
	m.outboxLag.Observe(seconds)
}

// RecordReconciliationDivergence incrementa divergências de saldo.
func (m *Metrics) RecordReconciliationDivergence() {
	if m == nil {
		return
	}
	m.reconciliation.Inc()
}

// RecordProcessingDuration observa a latência do canal.
func (m *Metrics) RecordProcessingDuration(channel string, seconds float64) {
	if m == nil {
		return
	}
	if seconds < 0 {
		seconds = 0
	}
	m.duration.WithLabelValues(channel).Observe(seconds)
}

var _ port.Recorder = (*Metrics)(nil)
