package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

func TestLiveAlwaysReportsOK(t *testing.T) {
	router := NewRouter(discardLogger(), NewHealthHandler(health.NewChecker([]health.Probe{
		health.NewProbe("postgres", func(context.Context) error { return errors.New("fora do ar") }),
	})))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, esperado 200 mesmo com dependência em falha", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", contentType)
	}

	var report health.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("corpo não é JSON válido: %v", err)
	}
	if report.Status != health.StatusOK {
		t.Errorf("status = %q, esperado %q", report.Status, health.StatusOK)
	}
}

func TestReadyReportsOKWhenDependenciesRespond(t *testing.T) {
	router := NewRouter(discardLogger(), NewHealthHandler(health.NewChecker([]health.Probe{
		health.NewProbe("postgres", func(context.Context) error { return nil }),
		health.NewProbe("sqs", func(context.Context) error { return nil }),
	})))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", rec.Code)
	}

	var report health.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("corpo não é JSON válido: %v", err)
	}
	if len(report.Checks) != 2 {
		t.Errorf("checks = %+v, esperadas duas dependências", report.Checks)
	}
}

func TestReadyReportsUnavailableWhenDependencyFails(t *testing.T) {
	router := NewRouter(discardLogger(), NewHealthHandler(health.NewChecker([]health.Probe{
		health.NewProbe("postgres", func(context.Context) error { return nil }),
		health.NewProbe("sqs", func(context.Context) error { return errors.New("fila inacessível") }),
	})))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperado 503", rec.Code)
	}

	var report health.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("corpo não é JSON válido: %v", err)
	}
	if report.Checks["sqs"].Error != "fila inacessível" {
		t.Errorf("detalhe do erro ausente: %+v", report.Checks)
	}
}
