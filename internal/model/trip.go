package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTripNotFound               = errors.New("trip not found")
	ErrDriverAlreadyHasActiveTrip = errors.New("driver already has an active trip")
	ErrTripAlreadyFinished        = errors.New("trip is already finished")
)

const (
	StatusActive    = "active"
	StatusCompleted = "completed"
)

// Trip — поездка. Поля соответствуют таблице trips из contracts/schema.md.
// колонка last_position_at появится в работе 3, поэтому здесь её пока нет.
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

// StatusChange — запись журнала trip_status_history.
// FromStatus пуст (nil) при создании поездки: прежнего статуса нет.
type StatusChange struct {
	TripID     uuid.UUID
	FromStatus *string
	ToStatus   string
	Reason     string
	ChangedAt  time.Time
}
