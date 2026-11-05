package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

// PgxQuerier объединяет методы *pgxpool.Pool и pgx.Tx
type PgxQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxManager управляет транзакциями так, что бизнес-код ничего не знает о pgx:
// транзакция живёт в context и достаётся репозиториями через getQuerier.
type TxManager struct {
	pool     *pgxpool.Pool
	isoLevel pgx.TxIsoLevel
}

// NewTxManager создаёт менеджер транзакций с уровнем изоляции READ COMMITTED.
func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{
		pool:     pool,
		isoLevel: pgx.ReadCommitted,
	}
}

// Do запускает fn внутри транзакции.
//
//   - fn вернула nil → COMMIT;
//   - fn вернула ошибку → ROLLBACK, ошибка пробрасывается наружу;
//   - fn запаниковала → ROLLBACK, паника пробрасывается дальше;
//   - если в ctx уже есть транзакция (вложенный Do), fn выполняется в ней же,
//     новая транзакция не открывается, а commit/rollback делает внешний Do.
func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: m.isoLevel})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// откат выполняем с контекстом без отмены: если контекст запроса уже
	// отменён (таймаут, клиент ушёл), ROLLBACK всё равно должен дойти до бд
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

// getQuerier возвращает pgx.Tx если контекст находится внутри транзакции,а иначе отдаёт сам пул
func getQuerier(ctx context.Context, pool *pgxpool.Pool) PgxQuerier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
