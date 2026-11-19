package trip

import (
	"errors"
	"fmt"
)

// Доменные ошибки поездки. Не знают про HTTP-статусы и SQL-коды:
// перевод в транспортный ответ выполняется на слое адаптеров.
var (
	ErrTripNotFound  = errors.New("trip not found")
	ErrTripCompleted = errors.New("trip already completed")
	ErrDriverBusy    = errors.New("driver already has an active trip")
)

// ValidationError описывает нарушение инварианта входных данных или модели:
// какое именно поле не прошло проверку и почему.
type ValidationError struct {
	Field  string
	Reason string
}

// NewValidationError создаёт ошибку валидации для конкретного поля.
func NewValidationError(field, reason string) *ValidationError {
	return &ValidationError{
		Field:  field,
		Reason: reason,
	}
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Reason
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}
