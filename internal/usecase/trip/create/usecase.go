package create

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// UseCase создаёт новую поездку и атомарно записывает переход статуса в журнал.
type UseCase struct {
	repo      tripRepository
	txManager txManager
	metrics   tripMetrics
	logger    *slog.Logger
	now       func() time.Time
	newID     func() uuid.UUID
}

// New создаёт юзкейс создания поездки.
func New(
	repo tripRepository,
	txManager txManager,
	metrics tripMetrics,
	logger *slog.Logger,
) *UseCase {
	return &UseCase{
		repo:      repo,
		txManager: txManager,
		metrics:   metrics,
		logger:    logger,
		now: func() time.Time {
			return time.Now().UTC().Truncate(time.Microsecond)
		},
		newID: uuid.New,
	}
}

// Execute проверяет инварианты через доменную модель и атомарно сохраняет
// поездку и первую запись в trip_status_history.
func (u *UseCase) Execute(ctx context.Context, in model.CreateTripInput) (*model.Trip, error) {
	startedAt := u.now()

	entity, err := domaintrip.New(domaintrip.NewParams{
		ID:         u.newID(),
		UserID:     in.UserID,
		DriverID:   in.DriverID,
		StartPoint: domaintrip.Coordinates{Latitude: in.StartLat, Longitude: in.StartLng},
		EndPoint:   domaintrip.Coordinates{Latitude: in.EndLat, Longitude: in.EndLng},
		Price:      in.Price,
		StartedAt:  startedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("validate new trip: %w", err)
	}

	dto := model.TripFromDomain(entity)

	err = u.txManager.Do(ctx, func(txCtx context.Context) error {
		if err := u.repo.Save(txCtx, &dto); err != nil {
			return err
		}
		return u.repo.AddStatusHistory(txCtx, model.StatusChange{
			TripID:    dto.ID,
			ToStatus:  string(domaintrip.StatusActive),
			Reason:    "trip created",
			ChangedAt: startedAt,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("create trip: %w", err)
	}

	if u.metrics != nil {
		u.metrics.IncTripCreated(ctx)
	}
	if u.logger != nil {
		u.logger.InfoContext(ctx, "trip created",
			"trip_id", dto.ID.String(),
			"driver_id", dto.DriverID.String(),
		)
	}

	return &dto, nil
}
