package listpositions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// отдаёт все сохранённые точки маршрута существующей поездки
type UseCase struct {
	repo routeRepository
}

// собираем юзкейс чтения маршрута
func New(repo routeRepository) *UseCase {
	return &UseCase{repo: repo}
}

// проверяем, что поездка есть в базе, и возвращаем её точки маршрута
func (u *UseCase) Execute(ctx context.Context, tripID uuid.UUID) ([]model.TripPosition, error) {
	if tripID == uuid.Nil {
		return nil, domaintrip.NewValidationError("tripId", "tripId must be a non-empty UUID")
	}

	if _, err := u.repo.GetByID(ctx, tripID); err != nil {
		return nil, fmt.Errorf("get trip for positions: %w", err)
	}

	positions, err := u.repo.ListPositions(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("list trip positions: %w", err)
	}

	return positions, nil
}
