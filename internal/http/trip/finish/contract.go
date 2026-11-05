//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

type useCase interface {
	Execute(ctx context.Context, id uuid.UUID) (*model.Trip, error)
}
