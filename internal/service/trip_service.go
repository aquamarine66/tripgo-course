package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

// интерфейс менеджера транзакций, чтобы сервис не зависел от pgx
type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// интерфейс репозитория, саму транзакцию он достаёт из контекста
type TripRepository interface {
	CreateTrip(ctx context.Context, trip *model.Trip) error
	AddStatusHistory(ctx context.Context, change model.StatusChange) error
	GetTripByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	FinishTrip(ctx context.Context, id uuid.UUID, finishedAt time.Time) (*model.Trip, error)
}

// входные данные для создания поездки после валидации в хендлере
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

// создаёт поездку и сразу пишет первую запись в историю в одной транзакции
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

// завершает поездку и фиксирует переход статуса в журнале в одной транзакции
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

// обрезаем время до микросекунд, чтобы оно совпадало с тем, как postgres хранит timestamptz
func currentTime() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
