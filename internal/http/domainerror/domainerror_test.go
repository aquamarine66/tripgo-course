package domainerror_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
)

func TestProblem_Mapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantType   string
		wantDetail string
	}{
		{
			name:       "validation error maps to 400 invalid_request",
			err:        domaintrip.NewValidationError("price", "must be non-negative"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantType:   "https://tripgo.example/problems/invalid-request",
			wantDetail: "price: must be non-negative",
		},
		{
			name:       "invalid param format error maps to 400 invalid_request",
			err:        &api.InvalidParamFormatError{ParamName: "tripId", Err: errors.New("bad uuid")},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantType:   "https://tripgo.example/problems/invalid-request",
			wantDetail: "Parameter tripId has invalid format",
		},
		{
			name:       "wrapped ErrTripNotFound maps to 404 trip_not_found",
			err:        fmt.Errorf("get trip: %w", domaintrip.ErrTripNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
			wantType:   "https://tripgo.example/problems/trip-not-found",
			wantDetail: "Trip was not found",
		},
		{
			name:       "wrapped ErrTripCompleted maps to 409 trip_completed",
			err:        fmt.Errorf("finish trip: %w", domaintrip.ErrTripCompleted),
			wantStatus: http.StatusConflict,
			wantCode:   "trip_completed",
			wantType:   "https://tripgo.example/problems/trip-completed",
			wantDetail: "Operation is not allowed for a completed trip",
		},
		{
			name:       "wrapped ErrDriverBusy maps to 409 driver_busy",
			err:        fmt.Errorf("create trip: %w", domaintrip.ErrDriverBusy),
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
			wantType:   "https://tripgo.example/problems/driver-busy",
			wantDetail: "Driver already has an active trip",
		},
		{
			name:       "unexpected repository error maps to 500 without leaking SQL details",
			err:        errors.New("pq: relation \"trips\" does not exist (SELECT * FROM trips)"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantType:   "https://tripgo.example/problems/internal-error",
			wantDetail: "Internal server error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, prob := domainerror.Problem(tc.err, "/api/v1/trips")
			if status != tc.wantStatus {
				t.Fatalf("status=%d, want %d", status, tc.wantStatus)
			}
			if int(prob.Status) != tc.wantStatus {
				t.Fatalf("prob.Status=%d, want %d", prob.Status, tc.wantStatus)
			}
			if prob.Code != tc.wantCode {
				t.Fatalf("prob.Code=%q, want %q", prob.Code, tc.wantCode)
			}
			if prob.Type != tc.wantType {
				t.Fatalf("prob.Type=%q, want %q", prob.Type, tc.wantType)
			}
			if prob.Detail == nil || *prob.Detail != tc.wantDetail {
				t.Fatalf("prob.Detail=%v, want %q", prob.Detail, tc.wantDetail)
			}
			if prob.Instance == nil || *prob.Instance != "/api/v1/trips" {
				t.Fatalf("prob.Instance=%v, want /api/v1/trips", prob.Instance)
			}
		})
	}
}

func TestWrite_DoesNotLeakInternalErrorInHTTPBody(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", nil)

	domainerror.Write(rec, req, nil, errors.New("FATAL SQL: SELECT password FROM secret_table"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type=%q, want application/problem+json", ct)
	}
	bodyStr := rec.Body.String()
	if strings.Contains(bodyStr, "secret_table") || strings.Contains(bodyStr, "FATAL SQL") {
		t.Fatalf("internal error details leaked into response body: %s", bodyStr)
	}

	var prob api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("unmarshal problem: %v", err)
	}
	if prob.Code != "internal_error" {
		t.Fatalf("prob.Code=%q, want internal_error", prob.Code)
	}
}
