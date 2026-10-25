package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aquamarine66/tripgo-course/internal/model"
	"github.com/aquamarine66/tripgo-course/internal/service"
)

var _ ServerInterface = (*TripHandler)(nil)

type TripHandler struct {
	service service.TripService
	pool    *pgxpool.Pool
}

func NewTripHandler(service service.TripService, pool *pgxpool.Pool) *TripHandler {
	return &TripHandler{
		service: service,
		pool:    pool,
	}
}

// CreateTrip (POST /api/v1/trips)
func (h *TripHandler) CreateTrip(w http.ResponseWriter, r *http.Request, params CreateTripParams) {
	var body TripData
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteProblem(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body", r.URL.Path)
		return
	}

	// валидация входных данных
	if body.Price < 0 {
		WriteProblem(w, http.StatusBadRequest, "Bad Request", "Price cannot be negative", r.URL.Path)
		return
	}
	if body.StartPoint.Latitude < -90 || body.StartPoint.Latitude > 90 ||
		body.StartPoint.Longitude < -180 || body.StartPoint.Longitude > 180 ||
		body.EndPoint.Latitude < -90 || body.EndPoint.Latitude > 90 ||
		body.EndPoint.Longitude < -180 || body.EndPoint.Longitude > 180 {
		WriteProblem(w, http.StatusBadRequest, "Bad Request", "Coordinates out of bounds", r.URL.Path)
		return
	}

	trip, err := h.service.CreateTrip(
		r.Context(),
		body.UserId,
		body.DriverId,
		body.Price,
		body.StartPoint.Latitude,
		body.StartPoint.Longitude,
		body.EndPoint.Latitude,
		body.EndPoint.Longitude,
	)
	if err != nil {
		if errors.Is(err, model.ErrDriverAlreadyHasActiveTrip) {
			WriteProblem(w, http.StatusConflict, "Conflict", "Driver already has an active trip", r.URL.Path)
			return
		}
		WriteProblem(w, http.StatusInternalServerError, "Internal Server Error", err.Error(), r.URL.Path)
		return
	}

	resp := mapTripToDTO(trip)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetTrip (GET /api/v1/trips/{tripId})
func (h *TripHandler) GetTrip(w http.ResponseWriter, r *http.Request, tripId TripId) {
	trip, err := h.service.GetTrip(r.Context(), tripId)
	if err != nil {
		if errors.Is(err, model.ErrTripNotFound) {
			WriteProblem(w, http.StatusNotFound, "Not Found", "Trip not found", r.URL.Path)
			return
		}
		WriteProblem(w, http.StatusInternalServerError, "Internal Server Error", err.Error(), r.URL.Path)
		return
	}

	resp := mapTripToDTO(trip)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// FinishTrip (POST /api/v1/trips/{tripId}/finish)
func (h *TripHandler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId TripId) {
	trip, err := h.service.FinishTrip(r.Context(), tripId)
	if err != nil {
		if errors.Is(err, model.ErrTripNotFound) {
			WriteProblem(w, http.StatusNotFound, "Not Found", "Trip not found", r.URL.Path)
			return
		}
		if errors.Is(err, model.ErrTripAlreadyFinished) {
			WriteProblem(w, http.StatusConflict, "Conflict", "Trip is already finished", r.URL.Path)
			return
		}
		WriteProblem(w, http.StatusInternalServerError, "Internal Server Error", err.Error(), r.URL.Path)
		return
	}

	resp := mapTripToDTO(trip)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ListTripPositions (GET /api/v1/trips/{tripId}/positions) — заглушка для лабораторной 1
func (h *TripHandler) ListTripPositions(w http.ResponseWriter, r *http.Request, tripId TripId) {
	w.WriteHeader(http.StatusNotImplemented)
}

// CreateTripPosition (POST /api/v1/trips/{tripId}/positions) — заглушка для лабораторной 1
func (h *TripHandler) CreateTripPosition(w http.ResponseWriter, r *http.Request, tripId TripId) {
	w.WriteHeader(http.StatusNotImplemented)
}

// Health (GET /health) — Liveness-проба
func (h *TripHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthResponse{Status: Ok})
}

// Ready (GET /ready) — Readiness-проба с проверкой подключения к базе данных
func (h *TripHandler) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.pool.Ping(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: Unavailable})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthResponse{Status: Ok})
}

// mapTripToDTO преобразует внутреннюю модель в структуру контракта OpenAPI
func mapTripToDTO(t *model.Trip) Trip {
	return Trip{
		Id:       t.ID,
		UserId:   t.UserID,
		DriverId: t.DriverID,
		Status:   TripStatus(t.Status),
		Price:    t.Price,
		StartPoint: Coordinates{
			Latitude:  t.StartLat,
			Longitude: t.StartLng,
		},
		EndPoint: Coordinates{
			Latitude:  t.EndLat,
			Longitude: t.EndLng,
		},
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
		LastPositionAt: t.LastPositionAt,
	}
}