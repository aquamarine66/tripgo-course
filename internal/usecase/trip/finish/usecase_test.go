package finish_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
	"github.com/aquamarine66/tripgo-course/internal/usecase/trip/finish"
)

func activeTrip(id uuid.UUID) *model.Trip {
	return &model.Trip{
		ID:        id,
		UserID:    uuid.New(),
		DriverID:  uuid.New(),
		Status:    string(domaintrip.StatusActive),
		Price:     1450,
		StartLat:  59.9398,
		StartLng:  30.3146,
		EndLat:    59.9290,
		EndLng:    30.3626,
		StartedAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	}
}

func TestUseCase_Execute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	id := uuid.New()

	txm.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	repo.EXPECT().
		GetByIDForUpdate(gomock.Any(), id).
		Return(activeTrip(id), nil)

	repo.EXPECT().
		Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tr *model.Trip) error {
			if tr.Status != string(domaintrip.StatusCompleted) {
				t.Errorf("Update received status %q, want %q", tr.Status, domaintrip.StatusCompleted)
			}
			if tr.FinishedAt == nil {
				t.Error("Update received nil FinishedAt")
			}
			return nil
		})

	repo.EXPECT().
		AddStatusHistory(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, ch model.StatusChange) error {
			if ch.TripID != id {
				t.Errorf("history TripID=%v, want %v", ch.TripID, id)
			}
			if ch.FromStatus == nil || *ch.FromStatus != string(domaintrip.StatusActive) {
				t.Errorf("history FromStatus=%v, want %q", ch.FromStatus, domaintrip.StatusActive)
			}
			if ch.ToStatus != string(domaintrip.StatusCompleted) {
				t.Errorf("history ToStatus=%q, want %q", ch.ToStatus, domaintrip.StatusCompleted)
			}
			return nil
		})

	metrics.EXPECT().IncTripCompleted(gomock.Any())

	uc := finish.New(repo, txm, metrics, nil)
	got, err := uc.Execute(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != string(domaintrip.StatusCompleted) {
		t.Fatalf("expected status %q, got %q", domaintrip.StatusCompleted, got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatal("expected non-nil FinishedAt")
	}
}

func TestUseCase_Execute_AlreadyCompleted(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	id := uuid.New()
	tr := activeTrip(id)
	finishedAt := time.Date(2026, 8, 24, 12, 18, 0, 0, time.UTC)
	tr.Status = string(domaintrip.StatusCompleted)
	tr.FinishedAt = &finishedAt

	txm.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(tr, nil)

	uc := finish.New(repo, txm, metrics, nil)
	_, err := uc.Execute(context.Background(), id)
	if !errors.Is(err, domaintrip.ErrTripCompleted) {
		t.Fatalf("expected ErrTripCompleted, got %v", err)
	}
}

func TestUseCase_Execute_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	id := uuid.New()

	txm.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(nil, domaintrip.ErrTripNotFound)

	uc := finish.New(repo, txm, metrics, nil)
	_, err := uc.Execute(context.Background(), id)
	if !errors.Is(err, domaintrip.ErrTripNotFound) {
		t.Fatalf("expected ErrTripNotFound, got %v", err)
	}
}

func TestUseCase_Execute_UpdateError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	id := uuid.New()
	errDB := errors.New("update failed")

	txm.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(activeTrip(id), nil)
	repo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errDB)

	uc := finish.New(repo, txm, metrics, nil)
	_, err := uc.Execute(context.Background(), id)
	if !errors.Is(err, errDB) {
		t.Fatalf("expected %v, got %v", errDB, err)
	}
}

func TestUseCase_Execute_NilUUID(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	uc := finish.New(repo, txm, metrics, nil)
	_, err := uc.Execute(context.Background(), uuid.Nil)
	var valErr *domaintrip.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}
