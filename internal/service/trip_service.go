package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

// TxManager — то, что сервису нужно от менеджера транзакций.
// Про pgx, пулы и сами транзакции сервис ничего не знает.
type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// TripRepository — хранилище поездок. Транзакция аргументом не передаётся:
// репозиторий сам достаёт её из контекста.
type TripRepository interface {
	CreateTrip(ctx context.Context, trip *model.Trip) error
	AddStatusHistory(ctx context.Context, change model.StatusChange) error
	GetTripByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	FinishTrip(ctx context.Context, id uuid.UUID, finishedAt time.Time) (*model.Trip, error)
}

// CreateTripInput — уже провалидированные данные для создания поездки.
type CreateTripInput struct {
	UserID   uuid.UUID
	DriverID uuid.UUID
	Price    int64
	StartLat float64
	StartLng float64
	EndLat   float64
	EndLng   float64
}

type TripService struct {
	repo      TripRepository
	txManager TxManager
}

func NewTripService(repo TripRepository, txManager TxManager) *TripService {
	return &TripService{
		repo:      repo,
		txManager: txManager,
	}
}

// CreateTrip атомарно пишет поездку в trips и запись в trip_status_history.
func (s *TripService) CreateTrip(ctx context.Context, in CreateTripInput) (*model.Trip, error) {
	now := currentTime()
	trip := &model.Trip{
		ID:        uuid.New(),
		UserID:    in.UserID,
		DriverID:  in.DriverID,
		Status:    model.StatusActive,
		Price:     in.Price,
		StartLat:  in.StartLat,
		StartLng:  in.StartLng,
		EndLat:    in.EndLat,
		EndLng:    in.EndLng,
		StartedAt: now,
	}

	err := s.txManager.Do(ctx, func(txCtx context.Context) error {
		if err := s.repo.CreateTrip(txCtx, trip); err != nil {
			return err
		}
		return s.repo.AddStatusHistory(txCtx, model.StatusChange{
			TripID:    trip.ID,
			ToStatus:  model.StatusActive,
			Reason:    "trip created",
			ChangedAt: now,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("create trip: %w", err)
	}

	return trip, nil
}

func (s *TripService) GetTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	trip, err := s.repo.GetTripByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}
	return trip, nil
}

// FinishTrip завершает поездку и пишет переход в журнал в одной транзакции.
func (s *TripService) FinishTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	now := currentTime()

	var trip *model.Trip
	err := s.txManager.Do(ctx, func(txCtx context.Context) error {
		finished, err := s.repo.FinishTrip(txCtx, id, now)
		if err != nil {
			return err
		}
		trip = finished

		from := model.StatusActive
		return s.repo.AddStatusHistory(txCtx, model.StatusChange{
			TripID:     id,
			FromStatus: &from,
			ToStatus:   model.StatusCompleted,
			Reason:     "trip finished",
			ChangedAt:  now,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("finish trip: %w", err)
	}

	return trip, nil
}

// currentTime возвращает текущее время в UTC с точностью до микросекунд —
// такой же, как у TIMESTAMPTZ в PostgreSQL. Иначе ответ на создание
// (наносекунды) отличался бы от того, что потом отдаёт GET (микросекунды).
func currentTime() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
