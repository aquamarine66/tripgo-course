package finish_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/http/trip/finish"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

func TestHandler_Handle_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	id := uuid.New()
	finishedAt := time.Date(2026, 8, 24, 12, 18, 0, 0, time.UTC)

	uc.EXPECT().
		Execute(gomock.Any(), id).
		Return(&model.Trip{
			ID:         id,
			UserID:     uuid.New(),
			DriverID:   uuid.New(),
			Status:     string(domaintrip.StatusCompleted),
			Price:      1450,
			StartLat:   59.9398,
			StartLng:   30.3146,
			EndLat:     59.9290,
			EndLng:     30.3626,
			StartedAt:  time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
			FinishedAt: &finishedAt,
		}, nil)

	h := finish.New(uc, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil)
	rec := httptest.NewRecorder()

	h.Handle(rec, req, id)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got api.Trip
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Status != api.Completed {
		t.Fatalf("status=%q, want completed", got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatal("expected non-nil FinishedAt")
	}
}

func TestHandler_Handle_AlreadyCompleted(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	id := uuid.New()
	uc.EXPECT().
		Execute(gomock.Any(), id).
		Return(nil, domaintrip.ErrTripCompleted)

	h := finish.New(uc, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil)
	rec := httptest.NewRecorder()

	h.Handle(rec, req, id)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409", rec.Code)
	}
	var prob api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("unmarshal problem: %v", err)
	}
	if prob.Code != "trip_completed" {
		t.Fatalf("code=%q, want trip_completed", prob.Code)
	}
}
