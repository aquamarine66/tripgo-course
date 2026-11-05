package transaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

// Querier объединяет методы *pgxpool.Pool и pgx.Tx, чтобы репозиторий
// одинаково работал как внутри транзакции, так и напрямую через пул.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Manager управляет транзакциями через context.Context, не раскрывая pgx бизнес-логике.
type Manager struct {
	pool     *pgxpool.Pool
	isoLevel pgx.TxIsoLevel
}

// NewManager создаёт менеджер транзакций с уровнем изоляции READ COMMITTED.
func NewManager(pool *pgxpool.Pool) *Manager {
	return &Manager{
		pool:     pool,
		isoLevel: pgx.ReadCommitted,
	}
}

// Do выполняет fn внутри транзакции:
//   - fn вернула nil → COMMIT;
//   - fn вернула ошибку → ROLLBACK и возврат ошибки;
//   - fn запаниковала → ROLLBACK и повторный panic;
//   - вложенный Do переиспользует уже открытую транзакцию из ctx.
func (m *Manager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: m.isoLevel})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	rollback := func() error {
		return tx.Rollback(context.WithoutCancel(ctx))
	}

	defer func() {
		if p := recover(); p != nil {
			_ = rollback()
			panic(p)
		}
	}()

	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		if rbErr := rollback(); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("rollback transaction: %w", rbErr))
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ExtractQuerier достаёт из контекста активную транзакцию, а если её нет — возвращает пул.
func ExtractQuerier(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
