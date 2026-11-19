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

// модель поездки по схеме таблицы trips (last_position_at добавится в третьей лабе)
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

// запись истории статусов, при создании поездки from_status остаётся пустым
type StatusChange struct {
	TripID     uuid.UUID
	FromStatus *string
	ToStatus   string
	Reason     string
	ChangedAt  time.Time
}
