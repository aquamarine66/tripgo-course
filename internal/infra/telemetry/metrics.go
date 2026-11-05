package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// ActiveTripsCounter — источник текущего числа активных поездок для gauge-метрики.
type ActiveTripsCounter interface {
	CountActive(ctx context.Context) (int64, error)
}

// Metrics объединяет все метрики сервиса из каталога observability.md.
type Metrics struct {
	HTTPRequestDuration metric.Float64Histogram
	HTTPActiveRequests  metric.Int64UpDownCounter
	TripsCreated        metric.Int64Counter
	TripsCompleted      metric.Int64Counter
}

// NewMetrics регистрирует все метрики ЛР2 в переданном Meter.
func NewMetrics(meter metric.Meter, activeCounter ActiveTripsCounter) (*Metrics, error) {
	reqDuration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of inbound HTTP requests in seconds"),
	)
	if err != nil {
		return nil, fmt.Errorf("create http.server.request.duration: %w", err)
	}

	activeReqs, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of in-flight HTTP requests"),
	)
	if err != nil {
		return nil, fmt.Errorf("create http.server.active_requests: %w", err)
	}

	tripsCreated, err := meter.Int64Counter(
		"tripgo.trips.created",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Total number of created trips"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.trips.created: %w", err)
	}

	tripsCompleted, err := meter.Int64Counter(
		"tripgo.trips.completed",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Total number of completed trips"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.trips.completed: %w", err)
	}

	if activeCounter != nil {
		_, err = meter.Int64ObservableGauge(
			"tripgo.trips.active",
			metric.WithUnit("{trip}"),
			metric.WithDescription("Current number of active trips"),
			metric.WithInt64Callback(func(ctx context.Context, observer metric.Int64Observer) error {
				count, err := activeCounter.CountActive(ctx)
				if err != nil {
					return nil
				}
				observer.Observe(count)
				return nil
			}),
		)
		if err != nil {
			return nil, fmt.Errorf("create tripgo.trips.active: %w", err)
		}
	}

	return &Metrics{
		HTTPRequestDuration: reqDuration,
		HTTPActiveRequests:  activeReqs,
		TripsCreated:        tripsCreated,
		TripsCompleted:      tripsCompleted,
	}, nil
}

// IncTripCreated увеличивает счётчик созданных поездок.
func (m *Metrics) IncTripCreated(ctx context.Context) {
	if m == nil {
		return
	}
	m.TripsCreated.Add(ctx, 1)
}

// IncTripCompleted увеличивает счётчик завершённых поездок.
func (m *Metrics) IncTripCompleted(ctx context.Context) {
	if m == nil {
		return
	}
	m.TripsCompleted.Add(ctx, 1)
}
