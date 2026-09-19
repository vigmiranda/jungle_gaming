// Package postgres provê o pool de conexões `pgx` e sua verificação de readiness.
package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/vigmi/backend-challenge-go/internal/config"
	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

// NewPool cria o pool e registra o fechamento no ciclo de vida do Fx.
//
// O pool é fechado no Stop, depois dos componentes que o utilizam, conforme a
// ordem inversa de registro dos hooks.
func NewPool(lc fx.Lifecycle, cfg config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.Postgres.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: DSN inválido: %w", err)
	}
	poolCfg.MaxConns = cfg.Postgres.MaxConns
	poolCfg.MinConns = cfg.Postgres.MinConns
	poolCfg.ConnConfig.ConnectTimeout = cfg.Postgres.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: não foi possível criar o pool: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			log.Info("postgres_pool_closing")
			pool.Close()
			return nil
		},
	})

	return pool, nil
}

// NewProbe verifica a disponibilidade do banco para o readiness.
func NewProbe(pool *pgxpool.Pool) health.Probe {
	return health.NewProbe("postgres", pool.Ping)
}
