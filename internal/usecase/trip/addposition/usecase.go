package addposition

import (
	"context"
	"fmt"
	"time"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// сохраняет новую точку маршрута, двигает last_position_at в транзакции и снимает поездку с очереди опроса
type UseCase struct {
	repo      positionRepository
	txManager txManager
	canceler  queueCanceler
	metrics   positionMetrics
	now       func() time.Time
}

// собираем юзкейс добавления координаты
func New(
	repo positionRepository,
	txManager txManager,
	canceler queueCanceler,
	metrics positionMetrics,
) *UseCase {
	return &UseCase{
		repo:      repo,
		txManager: txManager,
		canceler:  canceler,
		metrics:   metrics,
		now: func() time.Time {
			return time.Now().UTC().Truncate(time.Microsecond)
		},
	}
}

// валидируем координату, в одной транзакции пишем её и обновляем last_position_at, после коммита чистим очередь
func (u *UseCase) Execute(ctx context.Context, in model.CreatePositionInput) (*model.TripPosition, error) {
	entity, err := domaintrip.NewPosition(domaintrip.NewPositionParams{
		TripID: in.TripID,
		Coordinates: domaintrip.Coordinates{
			Latitude:  in.Latitude,
			Longitude: in.Longitude,
		},
		RecordedAt: in.RecordedAt,
		Now:        u.now(),
	})
	if err != nil {
		return nil, err
	}

	pos := model.PositionFromDomain(entity)

	err = u.txManager.Do(ctx, func(txCtx context.Context) error {
		stored, err := u.repo.GetByIDForUpdate(txCtx, in.TripID)
		if err != nil {
			return err
		}
		if err := stored.ToDomain().EnsureActive(); err != nil {
			return err
		}
		if err := u.repo.SavePosition(txCtx, &pos); err != nil {
			return err
		}
		return u.repo.UpdateLastPositionAt(txCtx, in.TripID, pos.RecordedAt)
	})
	if err != nil {
		return nil, fmt.Errorf("add trip position: %w", err)
	}

	if u.canceler != nil {
		u.canceler.Cancel(in.TripID)
	}
	if u.metrics != nil {
		u.metrics.IncPositionSaved(ctx)
	}

	return &pos, nil
}
