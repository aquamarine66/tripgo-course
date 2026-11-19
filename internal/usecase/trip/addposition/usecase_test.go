package addposition

import (
	"context"
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
	repo := NewMockpositionRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	canceler := NewMockqueueCanceler(ctrl)
	metrics := NewMockpositionMetrics(ctrl)

	tripID := uuid.New()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	recordedAt := now.Add(-10 * time.Second)

	txm.EXPECT().Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	gomock.InOrder(
		repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(&model.Trip{
			ID:     tripID,
			Status: string(domaintrip.StatusActive),
		}, nil),
		repo.EXPECT().SavePosition(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, pos *model.TripPosition) error {
				pos.ID = 42
				return nil
			}),
		repo.EXPECT().UpdateLastPositionAt(gomock.Any(), tripID, recordedAt).Return(nil),
	)

	canceler.EXPECT().Cancel(tripID)
	metrics.EXPECT().IncPositionSaved(gomock.Any())

	uc := New(repo, txm, canceler, metrics)
	uc.now = func() time.Time { return now }

	got, err := uc.Execute(t.Context(), model.CreatePositionInput{
		TripID:     tripID,
		Latitude:   59.9350,
		Longitude:  30.3350,
		RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if got.ID != 42 || got.TripID != tripID || !got.RecordedAt.Equal(recordedAt) {
		t.Fatalf("unexpected position: %+v", got)
	}
}

func TestUseCase_Execute_ValidationFailsBeforeTx(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMockpositionRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	canceler := NewMockqueueCanceler(ctrl)
	metrics := NewMockpositionMetrics(ctrl)

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	uc := New(repo, txm, canceler, metrics)
	uc.now = func() time.Time { return now }

	_, err := uc.Execute(t.Context(), model.CreatePositionInput{
		TripID:     uuid.New(),
		Latitude:   59.9350,
		Longitude:  30.3350,
		RecordedAt: now.Add(2 * time.Minute),
	})
	var valErr *domaintrip.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}

func TestUseCase_Execute_TripNotFoundAndCompleted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	t.Run("trip not found", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		repo := NewMockpositionRepository(ctrl)
		txm := NewMocktxManager(ctrl)

		tripID := uuid.New()
		txm.EXPECT().Do(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
				return fn(ctx)
			})
		repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(nil, domaintrip.ErrTripNotFound)

		uc := New(repo, txm, nil, nil)
		uc.now = func() time.Time { return now }

		_, err := uc.Execute(t.Context(), model.CreatePositionInput{
			TripID:     tripID,
			Latitude:   59.93,
			Longitude:  30.33,
			RecordedAt: now,
		})
		if !errors.Is(err, domaintrip.ErrTripNotFound) {
			t.Fatalf("expected ErrTripNotFound, got %v", err)
		}
	})

	t.Run("trip already completed", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		repo := NewMockpositionRepository(ctrl)
		txm := NewMocktxManager(ctrl)

		tripID := uuid.New()
		txm.EXPECT().Do(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
				return fn(ctx)
			})
		repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(&model.Trip{
			ID:     tripID,
			Status: string(domaintrip.StatusCompleted),
		}, nil)

		uc := New(repo, txm, nil, nil)
		uc.now = func() time.Time { return now }

		_, err := uc.Execute(t.Context(), model.CreatePositionInput{
			TripID:     tripID,
			Latitude:   59.93,
			Longitude:  30.33,
			RecordedAt: now,
		})
		if !errors.Is(err, domaintrip.ErrTripCompleted) {
			t.Fatalf("expected ErrTripCompleted, got %v", err)
		}
	})
}

func TestUseCase_Execute_RepositoryErrorsSkipCancelAndMetrics(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	dbErr := errors.New("connection lost")

	t.Run("save position error", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		repo := NewMockpositionRepository(ctrl)
		txm := NewMocktxManager(ctrl)
		canceler := NewMockqueueCanceler(ctrl)
		metrics := NewMockpositionMetrics(ctrl)

		tripID := uuid.New()
		txm.EXPECT().Do(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
				return fn(ctx)
			})
		repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(&model.Trip{
			ID:     tripID,
			Status: string(domaintrip.StatusActive),
		}, nil)
		repo.EXPECT().SavePosition(gomock.Any(), gomock.Any()).Return(dbErr)

		uc := New(repo, txm, canceler, metrics)
		uc.now = func() time.Time { return now }

		_, err := uc.Execute(t.Context(), model.CreatePositionInput{
			TripID:     tripID,
			Latitude:   59.93,
			Longitude:  30.33,
			RecordedAt: now,
		})
		if !errors.Is(err, dbErr) {
			t.Fatalf("expected wrapped dbErr, got %v", err)
		}
	})

	t.Run("update last_position_at error", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		repo := NewMockpositionRepository(ctrl)
		txm := NewMocktxManager(ctrl)
		canceler := NewMockqueueCanceler(ctrl)
		metrics := NewMockpositionMetrics(ctrl)

		tripID := uuid.New()
		txm.EXPECT().Do(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
				return fn(ctx)
			})
		repo.EXPECT().GetByIDForUpdate(gomock.Any(), tripID).Return(&model.Trip{
			ID:     tripID,
			Status: string(domaintrip.StatusActive),
		}, nil)
		repo.EXPECT().SavePosition(gomock.Any(), gomock.Any()).Return(nil)
		repo.EXPECT().UpdateLastPositionAt(gomock.Any(), tripID, now).Return(dbErr)

		uc := New(repo, txm, canceler, metrics)
		uc.now = func() time.Time { return now }

		_, err := uc.Execute(t.Context(), model.CreatePositionInput{
			TripID:     tripID,
			Latitude:   59.93,
			Longitude:  30.33,
			RecordedAt: now,
		})
		if !errors.Is(err, dbErr) {
			t.Fatalf("expected wrapped dbErr, got %v", err)
		}
	})
}
