package create_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
	"github.com/aquamarine66/tripgo-course/internal/usecase/trip/create"
)

func validInput() model.CreateTripInput {
	return model.CreateTripInput{
		UserID:   uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID: uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		Price:    1450,
		StartLat: 59.9398,
		StartLng: 30.3146,
		EndLat:   59.9290,
		EndLng:   30.3626,
	}
}

func TestUseCase_Execute_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	txm.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	var savedTrip model.Trip
	repo.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tr *model.Trip) error {
			savedTrip = *tr
			return nil
		})

	repo.EXPECT().
		AddStatusHistory(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, ch model.StatusChange) error {
			if ch.TripID != savedTrip.ID {
				t.Errorf("history TripID=%v, want %v", ch.TripID, savedTrip.ID)
			}
			if ch.FromStatus != nil {
				t.Errorf("expected nil FromStatus on creation, got %v", *ch.FromStatus)
			}
			if ch.ToStatus != string(domaintrip.StatusActive) {
				t.Errorf("history ToStatus=%q, want %q", ch.ToStatus, domaintrip.StatusActive)
			}
			return nil
		})

	metrics.EXPECT().IncTripCreated(gomock.Any())

	uc := create.New(repo, txm, metrics, nil)
	in := validInput()
	got, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if got.ID == uuid.Nil {
		t.Fatal("expected generated non-nil ID")
	}
	if got.UserID != in.UserID || got.DriverID != in.DriverID || got.Price != in.Price {
		t.Fatalf("unexpected trip fields: %+v", got)
	}
	if got.Status != string(domaintrip.StatusActive) {
		t.Fatalf("expected status %q, got %q", domaintrip.StatusActive, got.Status)
	}
	if got.StartedAt.IsZero() || got.StartedAt.Location() != time.UTC {
		t.Fatalf("expected non-zero UTC StartedAt, got %v", got.StartedAt)
	}
}

func TestUseCase_Execute_DriverBusy(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	txm.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})

	repo.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		Return(domaintrip.ErrDriverBusy)

	uc := create.New(repo, txm, metrics, nil)
	_, err := uc.Execute(context.Background(), validInput())
	if !errors.Is(err, domaintrip.ErrDriverBusy) {
		t.Fatalf("expected ErrDriverBusy, got %v", err)
	}
}

func TestUseCase_Execute_HistoryErrorAborts(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	errDB := errors.New("history insert failed")

	txm.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	repo.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil)
	repo.EXPECT().AddStatusHistory(gomock.Any(), gomock.Any()).Return(errDB)

	uc := create.New(repo, txm, metrics, nil)
	_, err := uc.Execute(context.Background(), validInput())
	if !errors.Is(err, errDB) {
		t.Fatalf("expected %v, got %v", errDB, err)
	}
}

func TestUseCase_Execute_ValidationFailsBeforeDB(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	txm := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	uc := create.New(repo, txm, metrics, nil)
	in := validInput()
	in.Price = -100

	_, err := uc.Execute(context.Background(), in)
	var valErr *domaintrip.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}
