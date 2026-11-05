package create_test

import (
	"bytes"
	"context"
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
	"github.com/aquamarine66/tripgo-course/internal/http/trip/create"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

func validRequestBody() string {
	return `{
		"user_id": "5cb72c04-7650-45c9-a79b-bcdba0631e0c",
		"driver_id": "8860b315-ec86-42eb-a17c-7c163d721ff5",
		"start_point": {"latitude": 59.9398, "longitude": 30.3146},
		"end_point": {"latitude": 59.9290, "longitude": 30.3626},
		"price": 1450
	}`
}

func TestHandler_Handle_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	tripID := uuid.MustParse("1f0a9c62-4a1c-4f2e-9d33-2a4bb0f0b111")
	startedAt := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	uc.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, in model.CreateTripInput) (*model.Trip, error) {
			return &model.Trip{
				ID:        tripID,
				UserID:    in.UserID,
				DriverID:  in.DriverID,
				Status:    string(domaintrip.StatusActive),
				Price:     in.Price,
				StartLat:  in.StartLat,
				StartLng:  in.StartLng,
				EndLat:    in.EndLat,
				EndLng:    in.EndLng,
				StartedAt: startedAt,
			}, nil
		})

	h, err := create.New(uc, nil)
	if err != nil {
		t.Fatalf("create.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validRequestBody()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Handle(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/trips/"+tripID.String() {
		t.Fatalf("Location=%q, want /api/v1/trips/%s", loc, tripID)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"finished_at":null`)) {
		t.Fatalf("expected explicit finished_at:null in body: %s", rec.Body.String())
	}
}

func TestHandler_Handle_DriverBusy(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	uc.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(nil, domaintrip.ErrDriverBusy)

	h, err := create.New(uc, nil)
	if err != nil {
		t.Fatalf("create.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validRequestBody()))
	rec := httptest.NewRecorder()

	h.Handle(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409", rec.Code)
	}
	var prob api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("unmarshal problem: %v", err)
	}
	if prob.Code != "driver_busy" {
		t.Fatalf("code=%q, want driver_busy", prob.Code)
	}
}

func TestHandler_Handle_InvalidRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "malformed JSON",
			body: `{"user_id":`,
		},
		{
			name: "missing required price",
			body: `{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":10,"longitude":10},"end_point":{"latitude":10,"longitude":10}}`,
		},
		{
			name: "unknown top-level field",
			body: `{"id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":10,"longitude":10},"end_point":{"latitude":10,"longitude":10},"price":100}`,
		},
		{
			name: "nil user_id UUID",
			body: `{"user_id":"00000000-0000-0000-0000-000000000000","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":10,"longitude":10},"end_point":{"latitude":10,"longitude":10},"price":100}`,
		},
		{
			name: "latitude out of bounds",
			body: `{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":95,"longitude":10},"end_point":{"latitude":10,"longitude":10},"price":100}`,
		},
		{
			name: "negative price",
			body: `{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5","start_point":{"latitude":10,"longitude":10},"end_point":{"latitude":10,"longitude":10},"price":-10}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			uc := NewMockuseCase(ctrl)

			h, err := create.New(uc, nil)
			if err != nil {
				t.Fatalf("create.New: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			h.Handle(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
			var prob api.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
				t.Fatalf("unmarshal problem: %v", err)
			}
			if prob.Code != "invalid_request" {
				t.Fatalf("code=%q, want invalid_request", prob.Code)
			}
		})
	}
}

func TestHandler_Handle_RepositoryErrorHidesDetails(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	uc.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("pq: syntax error at or near SELECT * FROM trips"))

	h, err := create.New(uc, nil)
	if err != nil {
		t.Fatalf("create.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(validRequestBody()))
	rec := httptest.NewRecorder()

	h.Handle(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "SELECT") || strings.Contains(rec.Body.String(), "pq:") {
		t.Fatalf("SQL details leaked in 500 response: %s", rec.Body.String())
	}
}
