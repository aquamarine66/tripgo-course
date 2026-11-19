package model

import (
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
)

// Trip — DTO поездки для передачи между слоями (хендлеры, юзкейсы, репозиторий).
type Trip struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	Status     string
	Price      int64
	StartLat   float64
	StartLng   float64
	EndLat     float64
	EndLng     float64
	StartedAt  time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// CreateTripInput — входные данные юзкейса создания поездки.
type CreateTripInput struct {
	UserID   uuid.UUID
	DriverID uuid.UUID
	Price    int64
	StartLat float64
	StartLng float64
	EndLat   float64
	EndLng   float64
}

// StatusChange — запись журнала trip_status_history.
// FromStatus пуст (nil) при создании поездки: прежнего статуса нет.
type StatusChange struct {
	TripID     uuid.UUID
	FromStatus *string
	ToStatus   string
	Reason     string
	ChangedAt  time.Time
}

// TripFromDomain преобразует доменную сущность поездки в межслойную модель.
func TripFromDomain(t *domaintrip.Trip) Trip {
	return Trip{
		ID:         t.ID,
		UserID:     t.UserID,
		DriverID:   t.DriverID,
		Status:     string(t.Status),
		Price:      t.Price,
		StartLat:   t.StartPoint.Latitude,
		StartLng:   t.StartPoint.Longitude,
		EndLat:     t.EndPoint.Latitude,
		EndLng:     t.EndPoint.Longitude,
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
	}
}

// ToDomain преобразует межслойную модель поездки в доменную сущность.
func (t Trip) ToDomain() *domaintrip.Trip {
	return &domaintrip.Trip{
		ID:         t.ID,
		UserID:     t.UserID,
		DriverID:   t.DriverID,
		StartPoint: domaintrip.Coordinates{Latitude: t.StartLat, Longitude: t.StartLng},
		EndPoint:   domaintrip.Coordinates{Latitude: t.EndLat, Longitude: t.EndLng},
		Price:      t.Price,
		Status:     domaintrip.Status(t.Status),
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
	}
}
