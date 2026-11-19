package trip_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
)

func validParams() domaintrip.NewParams {
	return domaintrip.NewParams{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: domaintrip.Coordinates{Latitude: 59.9398, Longitude: 30.3146},
		EndPoint:   domaintrip.Coordinates{Latitude: 59.9290, Longitude: 30.3626},
		Price:      1450,
		StartedAt:  time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(p *domaintrip.NewParams)
		wantField string
		wantErr   bool
	}{
		{
			name:    "valid trip",
			mutate:  func(_ *domaintrip.NewParams) {},
			wantErr: false,
		},
		{
			name:    "zero price is allowed",
			mutate:  func(p *domaintrip.NewParams) { p.Price = 0 },
			wantErr: false,
		},
		{
			name: "boundary coordinates are allowed",
			mutate: func(p *domaintrip.NewParams) {
				p.StartPoint = domaintrip.Coordinates{Latitude: -90, Longitude: -180}
				p.EndPoint = domaintrip.Coordinates{Latitude: 90, Longitude: 180}
			},
			wantErr: false,
		},
		{
			name:      "nil trip id",
			mutate:    func(p *domaintrip.NewParams) { p.ID = uuid.Nil },
			wantField: "id",
			wantErr:   true,
		},
		{
			name:      "nil user id",
			mutate:    func(p *domaintrip.NewParams) { p.UserID = uuid.Nil },
			wantField: "user_id",
			wantErr:   true,
		},
		{
			name:      "nil driver id",
			mutate:    func(p *domaintrip.NewParams) { p.DriverID = uuid.Nil },
			wantField: "driver_id",
			wantErr:   true,
		},
		{
			name:      "negative price",
			mutate:    func(p *domaintrip.NewParams) { p.Price = -1 },
			wantField: "price",
			wantErr:   true,
		},
		{
			name:      "start latitude below -90",
			mutate:    func(p *domaintrip.NewParams) { p.StartPoint.Latitude = -90.001 },
			wantField: "start_point.latitude",
			wantErr:   true,
		},
		{
			name:      "start latitude NaN",
			mutate:    func(p *domaintrip.NewParams) { p.StartPoint.Latitude = math.NaN() },
			wantField: "start_point.latitude",
			wantErr:   true,
		},
		{
			name:      "start longitude above 180",
			mutate:    func(p *domaintrip.NewParams) { p.StartPoint.Longitude = 180.001 },
			wantField: "start_point.longitude",
			wantErr:   true,
		},
		{
			name:      "end latitude above 90",
			mutate:    func(p *domaintrip.NewParams) { p.EndPoint.Latitude = 91 },
			wantField: "end_point.latitude",
			wantErr:   true,
		},
		{
			name:      "end longitude below -180",
			mutate:    func(p *domaintrip.NewParams) { p.EndPoint.Longitude = -181 },
			wantField: "end_point.longitude",
			wantErr:   true,
		},
		{
			name:      "zero started_at",
			mutate:    func(p *domaintrip.NewParams) { p.StartedAt = time.Time{} },
			wantField: "started_at",
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			params := validParams()
			tc.mutate(&params)

			got, err := domaintrip.New(params)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.Status != domaintrip.StatusActive {
					t.Fatalf("expected status %q, got %q", domaintrip.StatusActive, got.Status)
				}
				if got.FinishedAt != nil {
					t.Fatalf("expected nil FinishedAt for new trip, got %v", got.FinishedAt)
				}
				return
			}

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var valErr *domaintrip.ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if valErr.Field != tc.wantField {
				t.Fatalf("expected field %q, got %q (msg=%q)", tc.wantField, valErr.Field, valErr.Error())
			}
		})
	}
}

func TestTrip_Finish(t *testing.T) {
	t.Parallel()

	finishTime := time.Date(2026, 8, 24, 12, 18, 0, 0, time.UTC)

	t.Run("active trip transitions to completed", func(t *testing.T) {
		t.Parallel()

		tr, err := domaintrip.New(validParams())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := tr.Finish(finishTime); err != nil {
			t.Fatalf("Finish() unexpected error: %v", err)
		}
		if tr.Status != domaintrip.StatusCompleted {
			t.Fatalf("expected status %q, got %q", domaintrip.StatusCompleted, tr.Status)
		}
		if tr.FinishedAt == nil || !tr.FinishedAt.Equal(finishTime) {
			t.Fatalf("expected FinishedAt=%v, got %v", finishTime, tr.FinishedAt)
		}
	})

	t.Run("already completed trip returns ErrTripCompleted and preserves finished_at", func(t *testing.T) {
		t.Parallel()

		tr, err := domaintrip.New(validParams())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tr.Finish(finishTime); err != nil {
			t.Fatalf("first Finish() error: %v", err)
		}

		later := finishTime.Add(5 * time.Minute)
		err = tr.Finish(later)
		if !errors.Is(err, domaintrip.ErrTripCompleted) {
			t.Fatalf("expected ErrTripCompleted, got %v", err)
		}
		if !tr.FinishedAt.Equal(finishTime) {
			t.Fatalf("FinishedAt was overwritten: want %v, got %v", finishTime, *tr.FinishedAt)
		}
	})
}

func TestValidationError_ErrorFormatting(t *testing.T) {
	t.Parallel()

	withField := domaintrip.NewValidationError("price", "must be non-negative")
	if got := withField.Error(); got != "price: must be non-negative" {
		t.Fatalf("unexpected message: %q", got)
	}

	withoutField := domaintrip.NewValidationError("", "invalid request body")
	if got := withoutField.Error(); got != "invalid request body" {
		t.Fatalf("unexpected message: %q", got)
	}
}
