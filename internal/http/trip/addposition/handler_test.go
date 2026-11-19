package addposition

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

const (
	problemContentType = "application/problem+json"
	validPosPayload    = `{"coordinates":{"latitude":59.935,"longitude":30.335},"recorded_at":"2026-09-01T10:05:00Z"}`
)

func TestHandler_Handle_Created(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	uc := NewMockuseCase(ctrl)

	tripID := uuid.New()
	recorded := time.Date(2026, 9, 1, 10, 5, 0, 0, time.UTC)

	uc.EXPECT().Execute(gomock.Any(), model.CreatePositionInput{
		TripID:     tripID,
		Latitude:   59.935,
		Longitude:  30.335,
		RecordedAt: recorded,
	}).Return(&model.TripPosition{
		ID:         42,
		TripID:     tripID,
		Latitude:   59.935,
		Longitude:  30.335,
		RecordedAt: recorded,
	}, nil)

	h, err := New(uc, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID.String()+"/positions", strings.NewReader(validPosPayload))
	rec := httptest.NewRecorder()
	h.Handle(rec, req, tripID)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	var resp api.TripPosition
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Id != 42 || resp.TripId != tripID || !resp.RecordedAt.Equal(recorded) {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandler_Handle_BadRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{broken`},
		{name: "latitude out of range", body: `{"coordinates":{"latitude":120,"longitude":30.3},"recorded_at":"2026-09-01T10:05:00Z"}`},
		{name: "unknown field", body: `{"coordinates":{"latitude":59.9,"longitude":30.3},"recorded_at":"2026-09-01T10:05:00Z","extra":1}`},
		{name: "bad timestamp", body: `{"coordinates":{"latitude":59.9,"longitude":30.3},"recorded_at":"not-a-date"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			h, err := New(NewMockuseCase(ctrl), nil)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			tripID := uuid.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID.String()+"/positions", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.Handle(rec, req, tripID)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400, body=%s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != problemContentType {
				t.Fatalf("Content-Type=%q, want %s", ct, problemContentType)
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

func TestHandler_Handle_DomainAndInternalErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "not found", err: domaintrip.ErrTripNotFound, wantStatus: http.StatusNotFound, wantCode: "trip_not_found"},
		{name: "completed", err: domaintrip.ErrTripCompleted, wantStatus: http.StatusConflict, wantCode: "trip_completed"},
		{name: "internal", err: errors.New("sql syntax error"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			uc := NewMockuseCase(ctrl)
			uc.EXPECT().Execute(gomock.Any(), gomock.Any()).Return(nil, tc.err)

			h, err := New(uc, nil)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			tripID := uuid.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID.String()+"/positions", strings.NewReader(validPosPayload))
			rec := httptest.NewRecorder()
			h.Handle(rec, req, tripID)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d", rec.Code, tc.wantStatus)
			}
			var prob api.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
				t.Fatalf("unmarshal problem: %v", err)
			}
			if prob.Code != tc.wantCode {
				t.Fatalf("code=%q, want %q", prob.Code, tc.wantCode)
			}
			if strings.Contains(rec.Body.String(), "sql syntax error") {
				t.Fatalf("internal error detail leaked: %s", rec.Body.String())
			}
		})
	}
}
