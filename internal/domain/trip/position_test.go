package trip_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
)

func TestNewPosition_Success(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	loc := time.FixedZone("MSK", 3*3600)
	recorded := now.Add(time.Minute).In(loc)
	tripID := uuid.New()

	pos, err := domaintrip.NewPosition(domaintrip.NewPositionParams{
		TripID: tripID,
		Coordinates: domaintrip.Coordinates{
			Latitude:  59.9350,
			Longitude: 30.3350,
		},
		RecordedAt: recorded,
		Now:        now,
	})
	if err != nil {
		t.Fatalf("NewPosition() unexpected error: %v", err)
	}
	if pos.TripID != tripID {
		t.Fatalf("TripID=%s, want %s", pos.TripID, tripID)
	}
	if pos.RecordedAt.Location() != time.UTC || !pos.RecordedAt.Equal(recorded) {
		t.Fatalf("RecordedAt=%v, want UTC %v", pos.RecordedAt, recorded.UTC())
	}
}

func TestNewPosition_DefaultsNowWhenZero(t *testing.T) {
	t.Parallel()

	pos, err := domaintrip.NewPosition(domaintrip.NewPositionParams{
		TripID: uuid.New(),
		Coordinates: domaintrip.Coordinates{
			Latitude:  59.9350,
			Longitude: 30.3350,
		},
		RecordedAt: time.Now().UTC().Add(-5 * time.Second),
	})
	if err != nil {
		t.Fatalf("NewPosition() with zero Now unexpected error: %v", err)
	}
	if pos == nil {
		t.Fatal("expected non-nil position")
	}
}

func TestNewPosition_ValidationErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	validID := uuid.New()

	cases := []struct {
		name   string
		params domaintrip.NewPositionParams
	}{
		{
			name: "nil trip id",
			params: domaintrip.NewPositionParams{
				Coordinates: domaintrip.Coordinates{Latitude: 59.9, Longitude: 30.3},
				RecordedAt:  now,
				Now:         now,
			},
		},
		{
			name: "latitude out of range",
			params: domaintrip.NewPositionParams{
				TripID:      validID,
				Coordinates: domaintrip.Coordinates{Latitude: 95, Longitude: 30.3},
				RecordedAt:  now,
				Now:         now,
			},
		},
		{
			name: "longitude NaN",
			params: domaintrip.NewPositionParams{
				TripID:      validID,
				Coordinates: domaintrip.Coordinates{Latitude: 59.9, Longitude: math.NaN()},
				RecordedAt:  now,
				Now:         now,
			},
		},
		{
			name: "zero recorded_at",
			params: domaintrip.NewPositionParams{
				TripID:      validID,
				Coordinates: domaintrip.Coordinates{Latitude: 59.9, Longitude: 30.3},
				Now:         now,
			},
		},
		{
			name: "recorded_at more than 1 minute in future",
			params: domaintrip.NewPositionParams{
				TripID:      validID,
				Coordinates: domaintrip.Coordinates{Latitude: 59.9, Longitude: 30.3},
				RecordedAt:  now.Add(time.Minute + time.Second),
				Now:         now,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domaintrip.NewPosition(tc.params)
			var valErr *domaintrip.ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected *ValidationError, got %v", err)
			}
		})
	}
}
