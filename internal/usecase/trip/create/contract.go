//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

type (
	tripRepository interface {
		Save(ctx context.Context, trip *model.Trip) error
		AddStatusHistory(ctx context.Context, change model.StatusChange) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	tripMetrics interface {
		IncTripCreated(ctx context.Context)
	}
)
