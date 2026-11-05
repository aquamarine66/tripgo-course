package get

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// UseCase возвращает поездку по идентификатору.
type UseCase struct {
	repo tripRepository
}

// New создаёт юзкейс получения поездки.
func New(repo tripRepository) *UseCase {
	return &UseCase{repo: repo}
}

// Execute читает поездку по ID.
func (u *UseCase) Execute(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	if id == uuid.Nil {
		return nil, domaintrip.NewValidationError("tripId", "tripId must be a non-empty UUID")
	}

	trip, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}

	return trip, nil
}
