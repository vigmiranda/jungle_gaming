package httpserver

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"go.uber.org/fx/fxtest"

	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

func testConfig(port int) config.Config {
	return config.Config{HTTP: config.HTTP{
		Port:            port,
		ReadTimeout:     time.Second,
		WriteTimeout:    time.Second,
		IdleTimeout:     time.Second,
		ShutdownTimeout: 2 * time.Second,
	}}
}

func newTestServer(t *testing.T, port int) *Server {
	t.Helper()
	router := NewRouter(discardLogger(), NewHealthHandler(health.NewChecker(nil)))
	return NewServer(testConfig(port), discardLogger(), router)
}

func TestServerServesAndShutsDown(t *testing.T) {
	server := newTestServer(t, 0)

	if addr := server.Addr(); addr != "" {
		t.Errorf("Addr antes do start = %q, esperado vazio", addr)
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start devolveu erro: %v", err)
	}

	resp, err := http.Get("http://" + server.Addr() + "/health/live")
	if err != nil {
		t.Fatalf("requisição falhou: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, esperado 200", resp.StatusCode)
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("Stop devolveu erro: %v", err)
	}
	if _, err := http.Get("http://" + server.Addr() + "/health/live"); err == nil {
		t.Error("servidor deveria recusar conexões após o shutdown")
	}
}

func TestServerStopHonoursCancelledContext(t *testing.T) {
	server := newTestServer(t, 0)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start devolveu erro: %v", err)
	}

	// O Fx cancela o contexto de stop ao exceder seu próprio timeout; o shutdown
	// precisa continuar valendo pelo prazo configurado no servidor.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := server.Stop(ctx); err != nil {
		t.Errorf("Stop devolveu erro com contexto cancelado: %v", err)
	}
}

func TestRegisterBindsServerToLifecycle(t *testing.T) {
	server := newTestServer(t, 0)
	lifecycle := fxtest.NewLifecycle(t)

	Register(lifecycle, server)
	lifecycle.RequireStart()

	if server.Addr() == "" {
		t.Error("o hook de start deveria ter aberto o listener")
	}

	lifecycle.RequireStop()

	if _, err := http.Get("http://" + server.Addr() + "/health/live"); err == nil {
		t.Error("o hook de stop deveria ter encerrado o servidor")
	}
}

func TestServerStartFailsWhenPortIsBusy(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("não foi possível ocupar uma porta: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	server := newTestServer(t, port)

	if err := server.Start(context.Background()); err == nil {
		_ = server.Stop(context.Background())
		t.Fatal("esperava falha ao escutar em porta ocupada")
	}
}
