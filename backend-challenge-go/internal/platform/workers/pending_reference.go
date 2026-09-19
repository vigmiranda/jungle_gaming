package workers

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/config"
)

// PendingReferenceWorker retoma PENDING_REFERENCE com backoff (ADR-009).
type PendingReferenceWorker struct {
	resolve *usecase.ResolvePendingReferences
	log     *slog.Logger
	cfg     config.PendingReference
	cancel  context.CancelFunc
	done    sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// NewPendingReferenceWorker monta o worker.
func NewPendingReferenceWorker(
	resolve *usecase.ResolvePendingReferences,
	log *slog.Logger,
	cfg config.Config,
) *PendingReferenceWorker {
	return &PendingReferenceWorker{
		resolve: resolve,
		log:     log,
		cfg:     cfg.PendingReference,
	}
}

// RegisterPendingReference liga o worker ao ciclo de vida do Fx.
func RegisterPendingReference(lc fx.Lifecycle, worker *PendingReferenceWorker) {
	lc.Append(fx.Hook{
		OnStart: worker.Start,
		OnStop:  worker.Stop,
	})
}

// Start inicia o loop em background.
func (w *PendingReferenceWorker) Start(context.Context) error {
	if !w.cfg.Enabled {
		w.log.Info("pending_reference_worker_disabled")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	w.mu.Lock()
	w.cancel = cancel
	w.running = true
	w.mu.Unlock()

	w.done.Add(1)
	go func() {
		defer w.done.Done()
		w.loop(ctx)
	}()

	w.log.Info("pending_reference_worker_started",
		slog.Int("maxAttempts", w.cfg.MaxAttempts),
		slog.Duration("ttl", w.cfg.TTL),
	)
	return nil
}

// Stop interrompe novos ticks e aguarda o lote em andamento.
func (w *PendingReferenceWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	running := w.running
	cancel := w.cancel
	w.mu.Unlock()

	if !running {
		return nil
	}

	w.log.Info("pending_reference_worker_stopping")
	cancel()

	finished := make(chan struct{})
	go func() {
		w.done.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		w.log.Info("pending_reference_worker_stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *PendingReferenceWorker) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		processed, err := w.resolve.Tick(ctx)
		if err != nil && ctx.Err() == nil {
			w.log.Error("pending_reference_tick_failed", slog.String("error", err.Error()))
		}

		wait := w.cfg.PollInterval
		if processed > 0 {
			wait = 0
		}
		if wait <= 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
