package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

//go:generate go tool mockgen -source=contract.go -destination=mocks_test.go -package=positions

// источник устаревших активных поездок из бд
type staleTripFinder interface {
	FindStaleActive(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error)
}

// гейтвей для запроса координаты через push-service
type pushClient interface {
	RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
}

// метрики сканера, очереди и пула воркеров
type poolMetrics interface {
	IncStaleCheck(ctx context.Context, result string)
	SetPositionQueueSize(ctx context.Context, size int64)
	SetPositionWorkersActive(ctx context.Context, active int64)
	IncPositionRequest(ctx context.Context, result string)
	ObservePositionRequestDuration(ctx context.Context, seconds float64, result string)
}

// обёртка над тикером, чтобы подменять время в тестах без time.Sleep
type ticker interface {
	C() <-chan time.Time
	Stop()
}
