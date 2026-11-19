package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// сохраняем новую точку маршрута и заполняем выданные базой id и created_at
func (r *Repository) SavePosition(ctx context.Context, pos *model.TripPosition) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Insert("trip_positions").
		Columns("trip_id", "latitude", "longitude", "recorded_at").
		Values(pos.TripID, pos.Latitude, pos.Longitude, pos.RecordedAt).
		Suffix("RETURNING id, created_at").
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip position query: %w", err)
	}

	err = r.querier(ctx).QueryRow(ctx, query, args...).Scan(&pos.ID, &pos.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert trip position: %w", err)
	}

	return nil
}

// двигаем last_position_at только вперёд прямо в sql, чтобы опоздавшая координата не состарила поездку
func (r *Repository) UpdateLastPositionAt(ctx context.Context, tripID uuid.UUID, recordedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Update("trips").
		Set("last_position_at", sq.Expr(
			"CASE WHEN last_position_at IS NULL OR last_position_at < ? THEN ? ELSE last_position_at END",
			recordedAt, recordedAt,
		)).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": tripID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update last_position_at query: %w", err)
	}

	tag, err := r.querier(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update last_position_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domaintrip.ErrTripNotFound
	}

	return nil
}

// отдаём все точки маршрута поездки по возрастанию времени фиксации и id
func (r *Repository) ListPositions(ctx context.Context, tripID uuid.UUID) ([]model.TripPosition, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Select("id", "trip_id", "latitude", "longitude", "recorded_at", "created_at").
		From("trip_positions").
		Where(sq.Eq{"trip_id": tripID}).
		OrderBy("recorded_at ASC", "id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list trip positions query: %w", err)
	}

	rows, err := r.querier(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select trip positions: %w", err)
	}
	defer rows.Close()

	positions := make([]model.TripPosition, 0)
	for rows.Next() {
		var p model.TripPosition
		if err := rows.Scan(&p.ID, &p.TripID, &p.Latitude, &p.Longitude, &p.RecordedAt, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan trip position: %w", err)
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trip positions: %w", err)
	}

	return positions, nil
}

// выбираем активные поездки, по которым нет координат дольше staleAfter; самые старые идут первыми
func (r *Repository) FindStaleActive(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Select("id", "driver_id", "COALESCE(last_position_at, started_at)").
		From("trips").
		Where(sq.Eq{"status": string(domaintrip.StatusActive)}).
		Where(sq.Expr("COALESCE(last_position_at, started_at) < now() - make_interval(secs => ?)", staleAfter.Seconds())).
		OrderBy("COALESCE(last_position_at, started_at) ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build find stale active trips query: %w", err)
	}

	rows, err := r.querier(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select stale active trips: %w", err)
	}
	defer rows.Close()

	trips := make([]model.StaleTrip, 0)
	for rows.Next() {
		var st model.StaleTrip
		if err := rows.Scan(&st.TripID, &st.DriverID, &st.LastActivityAt); err != nil {
			return nil, fmt.Errorf("scan stale active trip: %w", err)
		}
		trips = append(trips, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale active trips: %w", err)
	}

	return trips, nil
}
