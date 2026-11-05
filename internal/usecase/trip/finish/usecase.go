package finish

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// UseCase завершает активную поездку и записывает переход в журнал.
type UseCase struct {
	repo      tripRepository
	txManager txManager
	metrics   tripMetrics
	logger    *slog.Logger
	now       func() time.Time
}

// New создаёт юзкейс завершения поездки.
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
	}
}

// Execute читает поездку с блокировкой строки внутри транзакции, выполняет
// доменный переход Finish и сохраняет обновлённую поездку вместе с записью в журнале.
func (u *UseCase) Execute(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	if id == uuid.Nil {
		return nil, domaintrip.NewValidationError("tripId", "tripId must be a non-empty UUID")
	}

	finishedAt := u.now()

	var result model.Trip
	err := u.txManager.Do(ctx, func(txCtx context.Context) error {
		stored, err := u.repo.GetByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		fromStatus := stored.Status
		entity := stored.ToDomain()
		if err := entity.Finish(finishedAt); err != nil {
			return err
		}

		result = model.TripFromDomain(entity)
		if err := u.repo.Update(txCtx, &result); err != nil {
			return err
		}

		return u.repo.AddStatusHistory(txCtx, model.StatusChange{
			TripID:     id,
			FromStatus: &fromStatus,
			ToStatus:   result.Status,
			Reason:     "trip finished",
			ChangedAt:  finishedAt,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("finish trip: %w", err)
	}

	if u.metrics != nil {
		u.metrics.IncTripCompleted(ctx)
	}
	if u.logger != nil {
		u.logger.InfoContext(ctx, "trip completed",
			"trip_id", result.ID.String(),
			"driver_id", result.DriverID.String(),
		)
	}

	return &result, nil
}
