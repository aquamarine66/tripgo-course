package addposition

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=addposition

type positionRepository interface {
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	SavePosition(ctx context.Context, pos *model.TripPosition) error
	UpdateLastPositionAt(ctx context.Context, tripID uuid.UUID, recordedAt time.Time) error
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type queueCanceler interface {
	Cancel(tripID uuid.UUID)
}

type positionMetrics interface {
	IncPositionSaved(ctx context.Context)
}
