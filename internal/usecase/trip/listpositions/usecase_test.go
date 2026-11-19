package listpositions

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

func TestUseCase_Execute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMockrouteRepository(ctrl)

	tripID := uuid.New()
	expected := []model.TripPosition{
		{ID: 1, TripID: tripID, Latitude: 59.93, Longitude: 30.33, RecordedAt: time.Now().UTC()},
	}

	gomock.InOrder(
		repo.EXPECT().GetByID(gomock.Any(), tripID).Return(&model.Trip{ID: tripID}, nil),
		repo.EXPECT().ListPositions(gomock.Any(), tripID).Return(expected, nil),
	)

	uc := New(repo)
	got, err := uc.Execute(t.Context(), tripID)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("got %+v, want %+v", got, expected)
	}
}

func TestUseCase_Execute_Errors(t *testing.T) {
	t.Parallel()

	t.Run("nil trip id", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		uc := New(NewMockrouteRepository(ctrl))

		_, err := uc.Execute(t.Context(), uuid.Nil)
		var valErr *domaintrip.ValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("expected ValidationError, got %v", err)
		}
	})

	t.Run("trip not found", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		repo := NewMockrouteRepository(ctrl)
		tripID := uuid.New()
		repo.EXPECT().GetByID(gomock.Any(), tripID).Return(nil, domaintrip.ErrTripNotFound)

		uc := New(repo)
		_, err := uc.Execute(t.Context(), tripID)
		if !errors.Is(err, domaintrip.ErrTripNotFound) {
			t.Fatalf("expected ErrTripNotFound, got %v", err)
		}
	})

	t.Run("list positions failure", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		repo := NewMockrouteRepository(ctrl)
		tripID := uuid.New()
		dbErr := errors.New("query timeout")

		gomock.InOrder(
			repo.EXPECT().GetByID(gomock.Any(), tripID).Return(&model.Trip{ID: tripID}, nil),
			repo.EXPECT().ListPositions(gomock.Any(), tripID).Return(nil, dbErr),
		)

		uc := New(repo)
		_, err := uc.Execute(t.Context(), tripID)
		if !errors.Is(err, dbErr) {
			t.Fatalf("expected dbErr, got %v", err)
		}
	})
}
