package trip

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// статус жизненного цикла поездки
type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

const (
	minLatitude           = -90.0
	maxLatitude           = 90.0
	minLongitude          = -180.0
	maxLongitude          = 180.0
	maxFuturePositionSkew = time.Minute
)

// географические координаты точки в wgs 84
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

// проверяем, что широта и долгота попадают в допустимые границы
func (c Coordinates) Validate(fieldPrefix string) error {
	if math.IsNaN(c.Latitude) || math.IsInf(c.Latitude, 0) || c.Latitude < minLatitude || c.Latitude > maxLatitude {
		return NewValidationError(fieldPrefix+".latitude", "latitude must be between -90 and 90")
	}
	if math.IsNaN(c.Longitude) || math.IsInf(c.Longitude, 0) || c.Longitude < minLongitude || c.Longitude > maxLongitude {
		return NewValidationError(fieldPrefix+".longitude", "longitude must be between -180 and 180")
	}
	return nil
}

// доменная сущность поездки со своими инвариантами и переходами статусов
type Trip struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	DriverID       uuid.UUID
	StartPoint     Coordinates
	EndPoint       Coordinates
	Price          int64
	Status         Status
	StartedAt      time.Time
	FinishedAt     *time.Time
	LastPositionAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// входные параметры для создания новой поездки
type NewParams struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Coordinates
	EndPoint   Coordinates
	Price      int64
	StartedAt  time.Time
}

// создаём активную поездку после проверки всех доменных ограничений
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

// проверяем, что поездка ещё активна и может принимать новые действия
func (t *Trip) EnsureActive() error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	return nil
}

// переводим поездку в completed; повторный вызов вернёт ErrTripCompleted
func (t *Trip) Finish(at time.Time) error {
	if err := t.EnsureActive(); err != nil {
		return err
	}
	finishedAt := at.UTC()
	t.Status = StatusCompleted
	t.FinishedAt = &finishedAt
	t.UpdatedAt = finishedAt
	return nil
}

// точка маршрута поездки, зафиксированная устройством водителя
type Position struct {
	ID          int64
	TripID      uuid.UUID
	Coordinates Coordinates
	RecordedAt  time.Time
	CreatedAt   time.Time
}

// параметры создания точки маршрута с текущим временем для проверки рассинхрона
type NewPositionParams struct {
	TripID      uuid.UUID
	Coordinates Coordinates
	RecordedAt  time.Time
	Now         time.Time
}

// валидируем координаты и время фиксации точки (не пустое и не дальше минуты в будущем)
func NewPosition(p NewPositionParams) (*Position, error) {
	if p.TripID == uuid.Nil {
		return nil, NewValidationError("tripId", "tripId must be a non-empty UUID")
	}
	if err := p.Coordinates.Validate("coordinates"); err != nil {
		return nil, err
	}
	if p.RecordedAt.IsZero() {
		return nil, NewValidationError("recorded_at", "recorded_at is required")
	}

	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	if p.RecordedAt.After(now.Add(maxFuturePositionSkew)) {
		return nil, NewValidationError("recorded_at", "recorded_at cannot be more than 1 minute in the future")
	}

	return &Position{
		TripID:      p.TripID,
		Coordinates: p.Coordinates,
		RecordedAt:  p.RecordedAt.UTC(),
	}, nil
}
