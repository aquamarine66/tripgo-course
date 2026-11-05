package response

import (
	"encoding/json"
	"net/http"
	"time"

	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// TripDTO расширяет сгенерированный api.Trip так, чтобы nullable-поля
// finished_at и last_position_at всегда присутствовали в JSON (в том числе со значением null).
type TripDTO struct {
	api.Trip
	FinishedAt     *time.Time `json:"finished_at"`
	LastPositionAt *time.Time `json:"last_position_at"`
}

// FromModel преобразует межслойную модель поездки в DTO ответа по контракту OpenAPI.
func FromModel(t *model.Trip) TripDTO {
	var finishedAt *time.Time
	if t.FinishedAt != nil {
		f := t.FinishedAt.UTC()
		finishedAt = &f
	}

	base := api.Trip{
		Id:       t.ID,
		UserId:   t.UserID,
		DriverId: t.DriverID,
		Status:   api.TripStatus(t.Status),
		Price:    t.Price,
		StartPoint: api.Coordinates{
			Latitude:  t.StartLat,
			Longitude: t.StartLng,
		},
		EndPoint: api.Coordinates{
			Latitude:  t.EndLat,
			Longitude: t.EndLng,
		},
		StartedAt:  t.StartedAt.UTC(),
		FinishedAt: finishedAt,
	}

	return TripDTO{
		Trip:           base,
		FinishedAt:     finishedAt,
		LastPositionAt: nil,
	}
}

// WriteJSON отправляет структуру в JSON с заданным HTTP-статусом.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
