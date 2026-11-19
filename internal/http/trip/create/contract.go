//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

type useCase interface {
	Execute(ctx context.Context, in model.CreateTripInput) (*model.Trip, error)
}
