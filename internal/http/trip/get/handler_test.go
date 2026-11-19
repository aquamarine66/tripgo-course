package get_test

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
	"github.com/aquamarine66/tripgo-course/internal/http/trip/get"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

func TestHandler_Handle_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	id := uuid.New()
	uc.EXPECT().
		Execute(gomock.Any(), id).
		Return(&model.Trip{
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
		}, nil)

	h := get.New(uc, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil)
	rec := httptest.NewRecorder()

	h.Handle(rec, req, id)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var got api.Trip
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Id != id {
		t.Fatalf("got ID=%v, want %v", got.Id, id)
	}
}

func TestHandler_Handle_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	id := uuid.New()
	uc.EXPECT().
		Execute(gomock.Any(), id).
		Return(nil, domaintrip.ErrTripNotFound)

	h := get.New(uc, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil)
	rec := httptest.NewRecorder()

	h.Handle(rec, req, id)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
	var prob api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("unmarshal problem: %v", err)
	}
	if prob.Code != "trip_not_found" {
		t.Fatalf("code=%q, want trip_not_found", prob.Code)
	}
}
