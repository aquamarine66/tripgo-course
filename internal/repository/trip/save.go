package trip

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgconn"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// Save вставляет новую поездку в таблицу trips.
// Нарушение частичного уникального индекса на driver_id переводится в domaintrip.ErrDriverBusy.
func (r *Repository) Save(ctx context.Context, trip *model.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Insert("trips").
		Columns(
			"id", "user_id", "driver_id", "status", "price",
			"start_latitude", "start_longitude", "end_latitude", "end_longitude",
			"started_at", "finished_at",
		).
		Values(
			trip.ID, trip.UserID, trip.DriverID, trip.Status, trip.Price,
			trip.StartLat, trip.StartLng, trip.EndLat, trip.EndLng,
			trip.StartedAt, trip.FinishedAt,
		).
		Suffix("RETURNING created_at, updated_at").
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	err = r.querier(ctx).QueryRow(ctx, query, args...).Scan(&trip.CreatedAt, &trip.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == driverActiveTripIndex {
			return domaintrip.ErrDriverBusy
		}
		return fmt.Errorf("insert trip: %w", err)
	}

	return nil
}

// Update сохраняет изменённое состояние поездки (например, при завершении).
func (r *Repository) Update(ctx context.Context, trip *model.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Update("trips").
		Set("status", trip.Status).
		Set("finished_at", trip.FinishedAt).
		Set("updated_at", trip.UpdatedAt).
		Where(sq.Eq{"id": trip.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update trip query: %w", err)
	}

	tag, err := r.querier(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update trip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domaintrip.ErrTripNotFound
	}

	return nil
}

// AddStatusHistory записывает переход статуса в таблицу trip_status_history.
func (r *Repository) AddStatusHistory(ctx context.Context, change model.StatusChange) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason", "changed_at").
		Values(change.TripID, change.FromStatus, change.ToStatus, change.Reason, change.ChangedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status history query: %w", err)
	}

	if _, err := r.querier(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert status history: %w", err)
	}

	return nil
}
