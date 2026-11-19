package trip

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// GetByID читает поездку по её UUID.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select trip query: %w", err)
	}

	t, err := scanTrip(r.querier(ctx).QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domaintrip.ErrTripNotFound
		}
		return nil, fmt.Errorf("select trip by id: %w", err)
	}

	return t, nil
}

// GetByIDForUpdate читает поездку с блокировкой строки (SELECT ... FOR UPDATE)
// внутри текущей транзакции, чтобы защитить переход статуса от гонки.
func (r *Repository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id}).
		Suffix("FOR UPDATE").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select trip for update query: %w", err)
	}

	t, err := scanTrip(r.querier(ctx).QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domaintrip.ErrTripNotFound
		}
		return nil, fmt.Errorf("select trip for update: %w", err)
	}

	return t, nil
}

// CountActive возвращает текущее количество активных поездок (для gauge-метрики tripgo.trips.active).
func (r *Repository) CountActive(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Select("COUNT(*)").
		From("trips").
		Where(sq.Eq{"status": string(domaintrip.StatusActive)}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build count active trips query: %w", err)
	}

	var count int64
	if err := r.querier(ctx).QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active trips: %w", err)
	}

	return count, nil
}
