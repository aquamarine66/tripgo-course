package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

type TripRepository interface {
	CreateTrip(ctx context.Context, trip *model.Trip) error
	AddStatusHistory(ctx context.Context, tripID uuid.UUID, status string, changedAt time.Time) error
	GetTripByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	FinishTrip(ctx context.Context, id uuid.UUID, finishedAt time.Time) error
}

type tripRepository struct {
	pool *pgxpool.Pool
}

func NewTripRepository(pool *pgxpool.Pool) TripRepository {
	return &tripRepository{pool: pool}
}

func (r *tripRepository) CreateTrip(ctx context.Context, trip *model.Trip) error {
	q := getQuerier(ctx, r.pool)

	query := `
		INSERT INTO trips (
			id, user_id, driver_id, status, price,
			start_lat, start_lng, end_lat, end_lng,
			started_at, finished_at, last_position_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12
		)
	`

	_, err := q.Exec(ctx, query,
		trip.ID, trip.UserID, trip.DriverID, trip.Status, trip.Price,
		trip.StartLat, trip.StartLng, trip.EndLat, trip.EndLng,
		trip.StartedAt, trip.FinishedAt, trip.LastPositionAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// код 23505 — unique_violation (сработал частичный индекс idx_trips_driver_active)
			if pgErr.Code == "23505" {
				return model.ErrDriverAlreadyHasActiveTrip
			}
		}
		return fmt.Errorf("insert trip: %w", err)
	}

	return nil
}

func (r *tripRepository) AddStatusHistory(ctx context.Context, tripID uuid.UUID, status string, changedAt time.Time) error {
	q := getQuerier(ctx, r.pool)

	query := `
		INSERT INTO trip_status_history (trip_id, status, changed_at)
		VALUES ($1, $2, $3)
	`

	_, err := q.Exec(ctx, query, tripID, status, changedAt)
	if err != nil {
		return fmt.Errorf("insert status history: %w", err)
	}

	return nil
}

func (r *tripRepository) GetTripByID(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	q := getQuerier(ctx, r.pool)

	query := `
		SELECT 
			id, user_id, driver_id, status, price,
			start_lat, start_lng, end_lat, end_lng,
			started_at, finished_at, last_position_at
		FROM trips
		WHERE id = $1
	`

	row := q.QueryRow(ctx, query, id)

	var t model.Trip
	err := row.Scan(
		&t.ID, &t.UserID, &t.DriverID, &t.Status, &t.Price,
		&t.StartLat, &t.StartLng, &t.EndLat, &t.EndLng,
		&t.StartedAt, &t.FinishedAt, &t.LastPositionAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrTripNotFound
		}
		return nil, fmt.Errorf("select trip by id: %w", err)
	}

	return &t, nil
}

func (r *tripRepository) FinishTrip(ctx context.Context, id uuid.UUID, finishedAt time.Time) error {
	q := getQuerier(ctx, r.pool)

	// атомарно завершаем поездку только если она находится в статусе 'active'
	query := `
		UPDATE trips
		SET status = $1, finished_at = $2
		WHERE id = $3 AND status = $4
	`

	cmdTag, err := q.Exec(ctx, query, model.StatusCompleted, finishedAt, id, model.StatusActive)
	if err != nil {
		return fmt.Errorf("finish trip: %w", err)
	}

	// если ни одна строка не обновилась, проверяем причину (не найдена или уже завершена)
	if cmdTag.RowsAffected() == 0 {
		var status string
		checkErr := q.QueryRow(ctx, `SELECT status FROM trips WHERE id = $1`, id).Scan(&status)
		if checkErr != nil {
			if errors.Is(checkErr, pgx.ErrNoRows) {
				return model.ErrTripNotFound
			}
			return fmt.Errorf("check trip status: %w", checkErr)
		}

		if status == model.StatusCompleted {
			return model.ErrTripAlreadyFinished
		}

		return model.ErrTripNotFound
	}

	return nil
}