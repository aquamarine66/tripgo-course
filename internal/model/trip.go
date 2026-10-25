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
	ErrInvalidStatus              = errors.New("invalid trip status")
)

const (
	StatusActive    = "active"
	StatusCompleted = "completed"
)

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
}

type TripStatusHistory struct {
	ID        int64
	TripID    uuid.UUID
	Status    string
	ChangedAt time.Time
}