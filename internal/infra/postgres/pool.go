package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// параметры пула соединений postgres
type Config struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
}

// собираем стандартный трейсер otelpgx для спанов и метрик запросов к бд
func NewTracer(tp trace.TracerProvider, mp metric.MeterProvider) pgx.QueryTracer {
	opts := make([]otelpgx.Option, 0, 2)
	if tp != nil {
		opts = append(opts, otelpgx.WithTracerProvider(tp))
	}
	if mp != nil {
		opts = append(opts, otelpgx.WithMeterProvider(mp))
	}
	return otelpgx.NewTracer(opts...)
}

// создаём пул pgx с подключённым трейсером и сразу проверяем соединение через ping
func NewPool(ctx context.Context, cfg Config, queryTracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	poolCfg.PingTimeout = cfg.ConnectTimeout
	if queryTracer != nil {
		poolCfg.ConnConfig.Tracer = queryTracer
	}

	connectCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
