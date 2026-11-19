package listpositions

import (
	"context"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=listpositions

type routeRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	ListPositions(ctx context.Context, tripID uuid.UUID) ([]model.TripPosition, error)
}
