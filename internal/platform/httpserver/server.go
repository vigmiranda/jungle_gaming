package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/config"
)

// Server encapsula o servidor HTTP e seu ciclo de vida.
type Server struct {
	cfg      config.HTTP
	log      *slog.Logger
	http     *http.Server
	listener net.Listener
}

// NewServer monta o servidor a partir da configuração validada.
func NewServer(cfg config.Config, log *slog.Logger, router *chi.Mux) *Server {
	return &Server{
		cfg: cfg.HTTP,
		log: log,
		http: &http.Server{
			Handler:      router,
			ReadTimeout:  cfg.HTTP.ReadTimeout,
			WriteTimeout: cfg.HTTP.WriteTimeout,
			IdleTimeout:  cfg.HTTP.IdleTimeout,
		},
	}
}

// Start abre o listener e passa a atender requisições.
//
// O listener é aberto de forma síncrona para que uma porta ocupada falhe o
// start do Fx em vez de deixar o processo no ar sem servidor.
func (s *Server) Start(context.Context) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		return fmt.Errorf("http: não foi possível escutar na porta %d: %w", s.cfg.Port, err)
	}
	s.listener = listener

	go func() {
		if err := s.http.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("http_server_failed", slog.String("error", err.Error()))
		}
	}()

	s.log.Info("http_server_started", slog.String("addr", listener.Addr().String()))
	return nil
}

// Stop interrompe novas entradas e aguarda as requisições em andamento dentro
// do prazo configurado.
func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()

	s.log.Info("http_server_stopping")
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http: shutdown incompleto: %w", err)
	}
	return nil
}

// Addr devolve o endereço efetivo do listener, útil quando a porta é 0.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Register liga o servidor ao ciclo de vida do Fx.
func Register(lc fx.Lifecycle, server *Server) {
	lc.Append(fx.Hook{OnStart: server.Start, OnStop: server.Stop})
}
