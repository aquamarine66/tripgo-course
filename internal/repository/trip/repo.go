package trip

import (
	"context"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aquamarine66/tripgo-course/internal/infra/postgres/transaction"
)

// pgUniqueViolation — код ошибки PostgreSQL unique_violation.
const pgUniqueViolation = "23505"

// driverActiveTripIndex — частичный уникальный индекс из миграции 00001.
const driverActiveTripIndex = "trips_driver_active_uniq"

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

var tripColumns = []string{
	"id", "user_id", "driver_id", "status", "price",
	"start_latitude", "start_longitude", "end_latitude", "end_longitude",
	"started_at", "finished_at", "created_at", "updated_at",
}

// Repository — PostgreSQL-репозиторий поездок и журнала переходов статуса.
type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

// New создаёт репозиторий поездок.
func New(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	return &Repository{
		pool:         pool,
		queryTimeout: queryTimeout,
	}
}

func (r *Repository) querier(ctx context.Context) transaction.Querier {
	return transaction.ExtractQuerier(ctx, r.pool)
}
