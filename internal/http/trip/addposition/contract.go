package addposition

import (
	"context"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=addposition

type useCase interface {
	Execute(ctx context.Context, in model.CreatePositionInput) (*model.TripPosition, error)
}
