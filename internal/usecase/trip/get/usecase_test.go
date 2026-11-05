package get_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
	"github.com/aquamarine66/tripgo-course/internal/usecase/trip/get"
)

func TestUseCase_Execute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)

	id := uuid.New()
	expected := &model.Trip{
		ID:        id,
		UserID:    uuid.New(),
		DriverID:  uuid.New(),
		Status:    string(domaintrip.StatusActive),
		Price:     1450,
		StartLat:  59.9398,
		StartLng:  30.3146,
		EndLat:    59.9290,
		EndLng:    30.3626,
		StartedAt: time.Now().UTC(),
	}

	repo.EXPECT().GetByID(gomock.Any(), id).Return(expected, nil)

	uc := get.New(repo)
	got, err := uc.Execute(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != expected.ID || got.Price != expected.Price {
		t.Fatalf("got %+v, want %+v", got, expected)
	}
}

func TestUseCase_Execute_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)

	id := uuid.New()
	repo.EXPECT().GetByID(gomock.Any(), id).Return(nil, domaintrip.ErrTripNotFound)

	uc := get.New(repo)
	_, err := uc.Execute(context.Background(), id)
	if !errors.Is(err, domaintrip.ErrTripNotFound) {
		t.Fatalf("expected ErrTripNotFound, got %v", err)
	}
}

func TestUseCase_Execute_NilUUID(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)

	uc := get.New(repo)
	_, err := uc.Execute(context.Background(), uuid.Nil)
	var valErr *domaintrip.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}
