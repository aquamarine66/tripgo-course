package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
	"github.com/aquamarine66/tripgo-course/internal/service"
)

// ограничиваем размер тела запроса до 1 мб
const maxBodyBytes = 1 << 20

var _ ServerInterface = (*TripHandler)(nil)

// интерфейс бизнес-логики поездок для хендлера
type TripService interface {
	CreateTrip(ctx context.Context, in service.CreateTripInput) (*model.Trip, error)
	GetTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	FinishTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error)
}

// проверка доступности базы для /ready
type Pinger interface {
	Ping(ctx context.Context) error
}

type TripHandler struct {
	service        TripService
	db             Pinger
	readyTimeout   time.Duration
	tripDataSchema *openapi3.Schema
}

// создаёт хендлер и достаёт схему TripData из встроенного openapi для валидации запросов
func NewTripHandler(svc TripService, db Pinger, readyTimeout time.Duration) (*TripHandler, error) {
	spec, err := GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load openapi spec: %w", err)
	}

	ref, ok := spec.Components.Schemas["TripData"]
	if !ok || ref.Value == nil {
		return nil, errors.New("openapi spec: schema TripData not found")
	}

	return &TripHandler{
		service:        svc,
		db:             db,
		readyTimeout:   readyTimeout,
		tripDataSchema: ref.Value,
	}, nil
}

// создание новой поездки (POST /api/v1/trips)
func (h *TripHandler) CreateTrip(w http.ResponseWriter, r *http.Request, _ CreateTripParams) {
	body, err := h.decodeTripData(w, r)
	if err != nil {
		writeProblem(w, r, problemInvalidRequest, err.Error())
		return
	}

	trip, err := h.service.CreateTrip(r.Context(), service.CreateTripInput{
		UserID:   body.UserId,
		DriverID: body.DriverId,
		Price:    body.Price,
		StartLat: body.StartPoint.Latitude,
		StartLng: body.StartPoint.Longitude,
		EndLat:   body.EndPoint.Latitude,
		EndLng:   body.EndPoint.Longitude,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+trip.ID.String())
	writeJSON(w, http.StatusCreated, mapTripToDTO(trip))
}

// получение поездки по id (GET /api/v1/trips/{tripId})
func (h *TripHandler) GetTrip(w http.ResponseWriter, r *http.Request, tripId TripId) {
	trip, err := h.service.GetTrip(r.Context(), tripId)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, mapTripToDTO(trip))
}

// завершение поездки (POST /api/v1/trips/{tripId}/finish)
func (h *TripHandler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId TripId) {
	trip, err := h.service.FinishTrip(r.Context(), tripId)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, mapTripToDTO(trip))
}

// заглушка под ручку маршрута, сама логика будет в третьей лабе
func (h *TripHandler) ListTripPositions(w http.ResponseWriter, r *http.Request, _ TripId) {
	writeProblem(w, r, problemNotImplemented, "Trip positions are not implemented yet")
}

// заглушка под сохранение координат, появится в третьей лабе
func (h *TripHandler) CreateTripPosition(w http.ResponseWriter, r *http.Request, _ TripId) {
	writeProblem(w, r, problemNotImplemented, "Trip positions are not implemented yet")
}

// liveness-проба, в базу не ходит (GET /health)
func (h *TripHandler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: Ok})
}

// readiness-проба, пингует базу с таймаутом (GET /ready)
func (h *TripHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.readyTimeout)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, HealthResponse{Status: Unavailable})
		return
	}

	writeJSON(w, http.StatusOK, HealthResponse{Status: Ok})
}

// читает и валидирует тело запроса создания поездки, текст ошибки безопасен для клиента
func (h *TripHandler) decodeTripData(w http.ResponseWriter, r *http.Request) (*TripData, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errors.New("request body is too large")
		}
		return nil, errors.New("failed to read request body")
	}

	// проверяем, что пришёл валидный json
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, errors.New("request body must be valid JSON")
	}

	// сверяем тело со схемой из контракта: обязательные поля, лишние поля, диапазоны
	if err := h.tripDataSchema.VisitJSON(generic); err != nil {
		return nil, schemaErrorDetail(err)
	}

	// раскладываем в сгенерированную структуру, тут же проверяется формат uuid
	var body TripData
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return nil, errors.New("user_id and driver_id must be valid UUIDs")
	}

	// нулевой uuid тоже не пропускаем
	if body.UserId == uuid.Nil {
		return nil, errors.New("user_id must be a non-empty UUID")
	}
	if body.DriverId == uuid.Nil {
		return nil, errors.New("driver_id must be a non-empty UUID")
	}

	return &body, nil
}

// вытаскивает из ошибки валидации схемы короткое описание поля и причины
func schemaErrorDetail(err error) error {
	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		field := strings.Join(schemaErr.JSONPointer(), "/")
		if field == "" {
			return fmt.Errorf("request body: %s", schemaErr.Reason)
		}
		return fmt.Errorf("/%s: %s", field, schemaErr.Reason)
	}
	return errors.New("request body does not match the contract")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// обёртка над сгенерированным Trip, чтобы finished_at и last_position_at всегда
// попадали в json как null, а не пропадали из-за omitempty в сгенерированном типе
type tripResponse struct {
	Trip
	FinishedAt     *time.Time `json:"finished_at"`
	LastPositionAt *time.Time `json:"last_position_at"`
}

// собирает ответ по контракту openapi, время отдаём в utc
func mapTripToDTO(t *model.Trip) tripResponse {
	var finishedAt *time.Time
	if t.FinishedAt != nil {
		f := t.FinishedAt.UTC()
		finishedAt = &f
	}

	trip := Trip{
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
		StartedAt:      t.StartedAt.UTC(),
		FinishedAt:     finishedAt,
		LastPositionAt: nil,
	}

	return tripResponse{
		Trip:       trip,
		FinishedAt: finishedAt,
		// координатами начнём заниматься с третьей лабы, пока тут всегда null
		LastPositionAt: nil,
	}
}
