package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

// общий интерфейс для *pgxpool.Pool и pgx.Tx
type PgxQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// менеджер транзакций прячет pgx от сервиса и передаёт текущую транзакцию через контекст
type TxManager struct {
	pool     *pgxpool.Pool
	isoLevel pgx.TxIsoLevel
	timeout  time.Duration
}

// создаёт менеджер транзакций с уровнем изоляции read committed и таймаутом на служебные операции
func NewTxManager(pool *pgxpool.Pool, timeout time.Duration) *TxManager {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &TxManager{
		pool:     pool,
		isoLevel: pgx.ReadCommitted,
		timeout:  timeout,
	}
}

// запускает fn внутри транзакции: при ошибке или панике откатывает изменения,
// а если транзакция уже есть в контексте, просто переиспользует её
func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	beginCtx, beginCancel := context.WithTimeout(ctx, m.timeout)
	tx, err := m.pool.BeginTx(beginCtx, pgx.TxOptions{IsoLevel: m.isoLevel})
	beginCancel()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// откат запускаем на отдельном контексте с таймаутом, чтобы он успел дойти до базы даже после отмены запроса
	rollback := func() error {
		rbCtx, rbCancel := context.WithTimeout(context.WithoutCancel(ctx), m.timeout)
		defer rbCancel()
		return tx.Rollback(rbCtx)
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

	commitCtx, commitCancel := context.WithTimeout(ctx, m.timeout)
	defer commitCancel()
	if err := tx.Commit(commitCtx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// достаёт транзакцию из контекста, а если её там нет, возвращает обычный пул
func getQuerier(ctx context.Context, pool *pgxpool.Pool) PgxQuerier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
