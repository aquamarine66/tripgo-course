package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

// код ошибки postgres при нарушении уникального ограничения
const pgUniqueViolation = "23505"

// имя частичного уникального индекса на активную поездку водителя
const driverActiveTripIndex = "trips_driver_active_uniq"

// билдер sql с плейсхолдерами $1, $2 для postgres
var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

var tripColumns = []string{
	"id", "user_id", "driver_id", "status", "price",
	"start_latitude", "start_longitude", "end_latitude", "end_longitude",
	"started_at", "finished_at", "created_at", "updated_at",
}

type TripRepository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

// создаёт репозиторий с таймаутом на выполнение каждого запроса
func NewTripRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *TripRepository {
	return &TripRepository{pool: pool, queryTimeout: queryTimeout}
}

func (r *TripRepository) CreateTrip(ctx context.Context, trip *model.Trip) error {
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

	err = getQuerier(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&trip.CreatedAt, &trip.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		// ловим конфликт по частичному индексу, если у водителя уже идёт поездка
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == driverActiveTripIndex {
			return model.ErrDriverAlreadyHasActiveTrip
		}
		return fmt.Errorf("insert trip: %w", err)
	}

	return nil
}

func (r *TripRepository) AddStatusHistory(ctx context.Context, change model.StatusChange) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason", "changed_at").
		Values(change.TripID, change.FromStatus, change.ToStatus, change.Reason, change.ChangedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status history query: %w", err)
	}

	if _, err := getQuerier(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert status history: %w", err)
	}

	return nil
}

func (r *TripRepository) GetTripByID(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select trip query: %w", err)
	}

	t, err := scanTrip(getQuerier(ctx, r.pool).QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrTripNotFound
		}
		return nil, fmt.Errorf("select trip by id: %w", err)
	}

	return t, nil
}

// атомарно завершает активную поездку через условие в where, чтобы защититься от двойного finish
func (r *TripRepository) FinishTrip(ctx context.Context, id uuid.UUID, finishedAt time.Time) (*model.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	q := getQuerier(ctx, r.pool)

	query, args, err := psql.Update("trips").
		Set("status", model.StatusCompleted).
		Set("finished_at", finishedAt).
		Set("updated_at", finishedAt).
		Where(sq.Eq{"id": id, "status": model.StatusActive}).
		Suffix("RETURNING " + strings.Join(tripColumns, ", ")).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build finish trip query: %w", err)
	}

	t, err := scanTrip(q.QueryRow(ctx, query, args...))
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("finish trip: %w", err)
	}

	// ничего не обновилось, проверяем, поездки вообще нет или она уже завершена
	checkQuery, checkArgs, err := psql.Select("status").
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build check trip status query: %w", err)
	}

	var status string
	if err := q.QueryRow(ctx, checkQuery, checkArgs...).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrTripNotFound
		}
		return nil, fmt.Errorf("check trip status: %w", err)
	}

	if status == model.StatusCompleted {
		return nil, model.ErrTripAlreadyFinished
	}

	return nil, fmt.Errorf("finish trip: unexpected status %q", status)
}

func scanTrip(row pgx.Row) (*model.Trip, error) {
	var t model.Trip
	err := row.Scan(
		&t.ID, &t.UserID, &t.DriverID, &t.Status, &t.Price,
		&t.StartLat, &t.StartLng, &t.EndLat, &t.EndLng,
		&t.StartedAt, &t.FinishedAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
