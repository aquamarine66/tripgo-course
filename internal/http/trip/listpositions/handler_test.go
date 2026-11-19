package listpositions

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

func TestHandler_Handle_OKWithPositions(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	tripID := uuid.New()
	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 1, 10, 1, 0, 0, time.UTC)

	uc.EXPECT().Execute(gomock.Any(), tripID).Return([]model.TripPosition{
		{ID: 1, TripID: tripID, Latitude: 59.93, Longitude: 30.33, RecordedAt: t1},
		{ID: 2, TripID: tripID, Latitude: 59.94, Longitude: 30.34, RecordedAt: t2},
	}, nil)

	h := New(uc, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID.String()+"/positions", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, tripID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}

	var resp api.TripPositionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Positions) != 2 || resp.Positions[0].Id != 1 || resp.Positions[1].Id != 2 {
		t.Fatalf("unexpected positions: %+v", resp.Positions)
	}
}

func TestHandler_Handle_EmptyRouteReturnsJSONArray(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	tripID := uuid.New()
	uc.EXPECT().Execute(gomock.Any(), tripID).Return(nil, nil)

	h := New(uc, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID.String()+"/positions", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, tripID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"positions":[]`) {
		t.Fatalf("expected empty JSON array, got: %s", rec.Body.String())
	}
}

func TestHandler_Handle_Errors(t *testing.T) {
	t.Parallel()

	t.Run("trip not found", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		uc := NewMockuseCase(ctrl)
		tripID := uuid.New()
		uc.EXPECT().Execute(gomock.Any(), tripID).Return(nil, domaintrip.ErrTripNotFound)

		h := New(uc, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID.String()+"/positions", nil)
		rec := httptest.NewRecorder()
		h.Handle(rec, req, tripID)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404", rec.Code)
		}
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		uc := NewMockuseCase(ctrl)
		tripID := uuid.New()
		uc.EXPECT().Execute(gomock.Any(), tripID).Return(nil, errors.New("db down"))

		h := New(uc, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID.String()+"/positions", nil)
		rec := httptest.NewRecorder()
		h.Handle(rec, req, tripID)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d, want 500", rec.Code)
		}
	})
}
