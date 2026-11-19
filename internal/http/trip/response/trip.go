package response

import (
	"encoding/json"
	"net/http"
	"time"

	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

// расширяет api.Trip, чтобы finished_at и last_position_at всегда уходили в json (даже как null)
type TripDTO struct {
	api.Trip
	FinishedAt     *time.Time `json:"finished_at"`
	LastPositionAt *time.Time `json:"last_position_at"`
}

// переводим модель поездки в ответ по контракту openapi
func FromModel(t *model.Trip) TripDTO {
	var finishedAt *time.Time
	if t.FinishedAt != nil {
		f := t.FinishedAt.UTC()
		finishedAt = &f
	}

	var lastPositionAt *time.Time
	if t.LastPositionAt != nil {
		lp := t.LastPositionAt.UTC()
		lastPositionAt = &lp
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
		StartedAt:      t.StartedAt.UTC(),
		FinishedAt:     finishedAt,
		LastPositionAt: lastPositionAt,
	}

	return TripDTO{
		Trip:           base,
		FinishedAt:     finishedAt,
		LastPositionAt: lastPositionAt,
	}
}

// переводим одну точку маршрута в контрактный формат
func PositionFromModel(p *model.TripPosition) api.TripPosition {
	return api.TripPosition{
		Id:     p.ID,
		TripId: p.TripID,
		Coordinates: api.Coordinates{
			Latitude:  p.Latitude,
			Longitude: p.Longitude,
		},
		RecordedAt: p.RecordedAt.UTC(),
	}
}

// собираем обёртку списка точек маршрута (для пустого маршрута уходит пустой массив [])
func PositionsFromModel(positions []model.TripPosition) api.TripPositionsResponse {
	items := make([]api.TripPosition, 0, len(positions))
	for i := range positions {
		items = append(items, PositionFromModel(&positions[i]))
	}
	return api.TripPositionsResponse{Positions: items}
}

// пишем структуру в json с нужным http-статусом
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
