package trip

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// Status — статус жизненного цикла поездки.
type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

const (
	minLatitude  = -90.0
	maxLatitude  = 90.0
	minLongitude = -180.0
	maxLongitude = 180.0
)

// Coordinates — географические координаты точки (WGS 84).
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

// Validate проверяет, что широта и долгота лежат в допустимых диапазонах.
func (c Coordinates) Validate(fieldPrefix string) error {
	if math.IsNaN(c.Latitude) || math.IsInf(c.Latitude, 0) || c.Latitude < minLatitude || c.Latitude > maxLatitude {
		return NewValidationError(fieldPrefix+".latitude", "latitude must be between -90 and 90")
	}
	if math.IsNaN(c.Longitude) || math.IsInf(c.Longitude, 0) || c.Longitude < minLongitude || c.Longitude > maxLongitude {
		return NewValidationError(fieldPrefix+".longitude", "longitude must be between -180 and 180")
	}
	return nil
}

// Trip — доменная сущность поездки. Отвечает за собственные инварианты и
// допустимые переходы между статусами.
type Trip struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Coordinates
	EndPoint   Coordinates
	Price      int64
	Status     Status
	StartedAt  time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewParams — входные параметры для создания новой поездки.
type NewParams struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Coordinates
	EndPoint   Coordinates
	Price      int64
	StartedAt  time.Time
}

// New создаёт новую активную поездку и проверяет все доменные инварианты.
func New(p NewParams) (*Trip, error) {
	if p.ID == uuid.Nil {
		return nil, NewValidationError("id", "id must be a non-empty UUID")
	}
	if p.UserID == uuid.Nil {
		return nil, NewValidationError("user_id", "user_id must be a non-empty UUID")
	}
	if p.DriverID == uuid.Nil {
		return nil, NewValidationError("driver_id", "driver_id must be a non-empty UUID")
	}
	if p.Price < 0 {
		return nil, NewValidationError("price", "price must be non-negative")
	}
	if err := p.StartPoint.Validate("start_point"); err != nil {
		return nil, err
	}
	if err := p.EndPoint.Validate("end_point"); err != nil {
		return nil, err
	}
	if p.StartedAt.IsZero() {
		return nil, NewValidationError("started_at", "started_at is required")
	}

	startedAt := p.StartedAt.UTC()
	return &Trip{
		ID:         p.ID,
		UserID:     p.UserID,
		DriverID:   p.DriverID,
		StartPoint: p.StartPoint,
		EndPoint:   p.EndPoint,
		Price:      p.Price,
		Status:     StatusActive,
		StartedAt:  startedAt,
		CreatedAt:  startedAt,
		UpdatedAt:  startedAt,
	}, nil
}

// Finish переводит активную поездку в статус completed и фиксирует время завершения.
// Повторное завершение возвращает ErrTripCompleted.
func (t *Trip) Finish(at time.Time) error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	finishedAt := at.UTC()
	t.Status = StatusCompleted
	t.FinishedAt = &finishedAt
	t.UpdatedAt = finishedAt
	return nil
}
