package model

import (
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
)

// межслойная модель поездки для передачи между хендлерами, юзкейсами и репозиторием
type Trip struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	DriverID       uuid.UUID
	Status         string
	Price          int64
	StartLat       float64
	StartLng       float64
	EndLat         float64
	EndLng         float64
	StartedAt      time.Time
	FinishedAt     *time.Time
	LastPositionAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// входные данные для создания поездки
type CreateTripInput struct {
	UserID   uuid.UUID
	DriverID uuid.UUID
	Price    int64
	StartLat float64
	StartLng float64
	EndLat   float64
	EndLng   float64
}

// запись журнала переходов статуса; при создании FromStatus пуст (nil)
type StatusChange struct {
	TripID     uuid.UUID
	FromStatus *string
	ToStatus   string
	Reason     string
	ChangedAt  time.Time
}

// точка маршрута поездки из таблицы trip_positions
type TripPosition struct {
	ID         int64
	TripID     uuid.UUID
	Latitude   float64
	Longitude  float64
	RecordedAt time.Time
	CreatedAt  time.Time
}

// входные данные для сохранения новой координаты поездки
type CreatePositionInput struct {
	TripID     uuid.UUID
	Latitude   float64
	Longitude  float64
	RecordedAt time.Time
}

// активная поездка без свежих координат, попавшая в выборку сканера
type StaleTrip struct {
	TripID         uuid.UUID
	DriverID       uuid.UUID
	LastActivityAt time.Time
}

// переводим доменную сущность поездки в межслойную модель
func TripFromDomain(t *domaintrip.Trip) Trip {
	return Trip{
		ID:             t.ID,
		UserID:         t.UserID,
		DriverID:       t.DriverID,
		Status:         string(t.Status),
		Price:          t.Price,
		StartLat:       t.StartPoint.Latitude,
		StartLng:       t.StartPoint.Longitude,
		EndLat:         t.EndPoint.Latitude,
		EndLng:         t.EndPoint.Longitude,
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
		LastPositionAt: t.LastPositionAt,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

// собираем доменную сущность поездки из межслойной модели
func (t Trip) ToDomain() *domaintrip.Trip {
	return &domaintrip.Trip{
		ID:             t.ID,
		UserID:         t.UserID,
		DriverID:       t.DriverID,
		StartPoint:     domaintrip.Coordinates{Latitude: t.StartLat, Longitude: t.StartLng},
		EndPoint:       domaintrip.Coordinates{Latitude: t.EndLat, Longitude: t.EndLng},
		Price:          t.Price,
		Status:         domaintrip.Status(t.Status),
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
		LastPositionAt: t.LastPositionAt,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

// переводим доменную точку маршрута в межслойную модель
func PositionFromDomain(p *domaintrip.Position) TripPosition {
	return TripPosition{
		ID:         p.ID,
		TripID:     p.TripID,
		Latitude:   p.Coordinates.Latitude,
		Longitude:  p.Coordinates.Longitude,
		RecordedAt: p.RecordedAt,
		CreatedAt:  p.CreatedAt,
	}
}
