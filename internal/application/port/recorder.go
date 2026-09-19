package port

// Recorder registra sinais de observabilidade na borda e nos workers.
//
// A implementação vive em platform/metrics. Casos de uso e testes podem
// receber nil — nenhum sinal é emitido.
type Recorder interface {
	RecordTransaction(channel, status string, replay bool)
	RecordRetry(component string)
	RecordDLQ()
	RecordConflict(source string)
	RecordOutboxLag(seconds float64)
	RecordReconciliationDivergence()
	RecordProcessingDuration(channel string, seconds float64)
}
