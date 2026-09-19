package httpserver

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/platform/correlation"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestCorrelationReusesIncomingHeader(t *testing.T) {
	var seen string
	handler := Correlation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = correlation.FromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	req.Header.Set(correlation.Header, "  fornecido-pelo-cliente  ")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if seen != "fornecido-pelo-cliente" {
		t.Errorf("contexto = %q, esperado o identificador do cabeçalho", seen)
	}
	if got := rec.Header().Get(correlation.Header); got != "fornecido-pelo-cliente" {
		t.Errorf("cabeçalho devolvido = %q", got)
	}
}

func TestCorrelationGeneratesIDWhenAbsent(t *testing.T) {
	var seen string
	handler := Correlation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = correlation.FromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if seen == "" {
		t.Fatal("esperava um identificador gerado no contexto")
	}
	if got := rec.Header().Get(correlation.Header); got != seen {
		t.Errorf("cabeçalho = %q, contexto = %q; deveriam coincidir", got, seen)
	}
}

func TestRecoverConvertsPanicIntoInternalError(t *testing.T) {
	handler := Recover(discardLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("falha inesperada")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, esperado 500", rec.Code)
	}

	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo não é JSON válido: %v", err)
	}
	if body.Error != "internal_error" {
		t.Errorf("error = %q, esperado \"internal_error\"", body.Error)
	}
	if strings.Contains(rec.Body.String(), "falha inesperada") {
		t.Error("a resposta não deve expor o detalhe do panic")
	}
}

func TestRecoverPassesThroughWhenHandlerSucceeds(t *testing.T) {
	handler := Recover(discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, esperado 418", rec.Code)
	}
}

func TestRequestLoggerRecordsStatusAndCorrelation(t *testing.T) {
	var logged strings.Builder
	logger := slog.New(slog.NewJSONHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := Correlation(RequestLogger(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})))

	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", nil)
	req.Header.Set(correlation.Header, "corr-1")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	output := logged.String()
	for _, fragment := range []string{`"status":202`, `"correlationId":"corr-1"`, `"path":"/wagering/transactions"`} {
		if !strings.Contains(output, fragment) {
			t.Errorf("log não contém %s: %s", fragment, output)
		}
	}
}

func TestRequestLoggerUsesDebugForHealthChecks(t *testing.T) {
	var logged strings.Builder
	logger := slog.New(slog.NewJSONHandler(&logged, &slog.HandlerOptions{Level: slog.LevelInfo}))

	handler := RequestLogger(logger, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if logged.Len() != 0 {
		t.Errorf("health check não deveria logar em nível info: %s", logged.String())
	}
}

func TestStatusRecorderKeepsFirstStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusInternalServerError)

	if recorder.status != http.StatusCreated {
		t.Errorf("status registrado = %d, esperado 201", recorder.status)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status enviado = %d, esperado 201", rec.Code)
	}
}
