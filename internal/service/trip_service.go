package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
	"github.com/aquamarine66/tripgo-course/internal/repository"
)

type TripService interface {
	CreateTrip(
		ctx context.Context,
		userID, driverID uuid.UUID,
		price int64,
		startLat, startLng, endLat, endLng float64,
	) (*model.Trip, error)
	GetTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	FinishTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error)
}

type tripService struct {
	repo      repository.TripRepository
	txManager repository.TxManager
}

func NewTripService(repo repository.TripRepository, txManager repository.TxManager) TripService {
	return &tripService{
		repo:      repo,
		txManager: txManager,
	}
}

func (s *tripService) CreateTrip(
	ctx context.Context,
	userID, driverID uuid.UUID,
	price int64,
	startLat, startLng, endLat, endLng float64,
) (*model.Trip, error) {
	now := time.Now().UTC()
	trip := &model.Trip{
		ID:        uuid.New(),
		UserID:    userID,
		DriverID:  driverID,
		Status:    model.StatusActive,
		Price:     price,
		StartLat:  startLat,
		StartLng:  startLng,
		EndLat:    endLat,
		EndLng:    endLng,
		StartedAt: now,
	}

	err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.CreateTrip(txCtx, trip); err != nil {
			return err
		}
		return s.repo.AddStatusHistory(txCtx, trip.ID, model.StatusActive, now)
	})
	if err != nil {
		return nil, err
	}

	return trip, nil
}

func (s *tripService) GetTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	return s.repo.GetTripByID(ctx, id)
}

func (s *tripService) FinishTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	now := time.Now().UTC()

	err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.FinishTrip(txCtx, id, now); err != nil {
			return err
		}
		return s.repo.AddStatusHistory(txCtx, id, model.StatusCompleted, now)
	})
	if err != nil {
		return nil, err
	}

	return s.repo.GetTripByID(ctx, id)
}