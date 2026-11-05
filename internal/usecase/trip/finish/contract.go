//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

type (
	tripRepository interface {
		GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*model.Trip, error)
		Update(ctx context.Context, trip *model.Trip) error
		AddStatusHistory(ctx context.Context, change model.StatusChange) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	tripMetrics interface {
		IncTripCompleted(ctx context.Context)
	}
)
