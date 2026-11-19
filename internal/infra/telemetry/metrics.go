package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const attrResult = "result"

// набор всех метрик сервиса из каталога observability.md
type Metrics struct {
	HTTPRequestDuration metric.Float64Histogram
	HTTPActiveRequests  metric.Int64UpDownCounter
	TripsCreated        metric.Int64Counter
	TripsCompleted      metric.Int64Counter

	PositionsSaved          metric.Int64Counter
	StaleChecks             metric.Int64Counter
	PositionQueueSize       metric.Int64Gauge
	PositionWorkersActive   metric.Int64Gauge
	PositionRequests        metric.Int64Counter
	PositionRequestDuration metric.Float64Histogram
}

// регистрируем все метрики сервиса в переданном meter
func NewMetrics(meter metric.Meter) (*Metrics, error) {
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

	positionsSaved, err := meter.Int64Counter(
		"tripgo.positions.saved",
		metric.WithUnit("{position}"),
		metric.WithDescription("Total number of saved trip positions"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.positions.saved: %w", err)
	}

	staleChecks, err := meter.Int64Counter(
		"tripgo.stale_checks",
		metric.WithUnit("{check}"),
		metric.WithDescription("Outcomes of stale trip checks during scans"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.stale_checks: %w", err)
	}

	queueSize, err := meter.Int64Gauge(
		"tripgo.position_requests.queue.size",
		metric.WithUnit("{task}"),
		metric.WithDescription("Current number of tasks waiting in the position request queue"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.position_requests.queue.size: %w", err)
	}

	workersActive, err := meter.Int64Gauge(
		"tripgo.position_requests.workers.active",
		metric.WithUnit("{worker}"),
		metric.WithDescription("Current number of busy position request workers"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.position_requests.workers.active: %w", err)
	}

	posRequests, err := meter.Int64Counter(
		"tripgo.position_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Total number of position request attempts by outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.position_requests: %w", err)
	}

	posReqDuration, err := meter.Float64Histogram(
		"tripgo.position_requests.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Push Service position requests in seconds"),
	)
	if err != nil {
		return nil, fmt.Errorf("create tripgo.position_requests.duration: %w", err)
	}

	return &Metrics{
		HTTPRequestDuration:     reqDuration,
		HTTPActiveRequests:      activeReqs,
		TripsCreated:            tripsCreated,
		TripsCompleted:          tripsCompleted,
		PositionsSaved:          positionsSaved,
		StaleChecks:             staleChecks,
		PositionQueueSize:       queueSize,
		PositionWorkersActive:   workersActive,
		PositionRequests:        posRequests,
		PositionRequestDuration: posReqDuration,
	}, nil
}

// увеличиваем счётчик созданных поездок
func (m *Metrics) IncTripCreated(ctx context.Context) {
	if m == nil {
		return
	}
	m.TripsCreated.Add(ctx, 1)
}

// увеличиваем счётчик завершённых поездок
func (m *Metrics) IncTripCompleted(ctx context.Context) {
	if m == nil {
		return
	}
	m.TripsCompleted.Add(ctx, 1)
}

// увеличиваем счётчик сохранённых координат
func (m *Metrics) IncPositionSaved(ctx context.Context) {
	if m == nil {
		return
	}
	m.PositionsSaved.Add(ctx, 1)
}

// фиксируем исход постановки устаревшей поездки в очередь: queued, duplicate или dropped
func (m *Metrics) IncStaleCheck(ctx context.Context, result string) {
	if m == nil {
		return
	}
	m.StaleChecks.Add(ctx, 1, metric.WithAttributes(attribute.String(attrResult, result)))
}

// обновляем текущий размер очереди опроса
func (m *Metrics) SetPositionQueueSize(ctx context.Context, size int64) {
	if m == nil {
		return
	}
	m.PositionQueueSize.Record(ctx, size)
}

// обновляем число занятых воркеров пула
func (m *Metrics) SetPositionWorkersActive(ctx context.Context, active int64) {
	if m == nil {
		return
	}
	m.PositionWorkersActive.Record(ctx, active)
}

// фиксируем результат попытки запросить координату: success, error или dropped
func (m *Metrics) IncPositionRequest(ctx context.Context, result string) {
	if m == nil {
		return
	}
	m.PositionRequests.Add(ctx, 1, metric.WithAttributes(attribute.String(attrResult, result)))
}

// записываем длительность вызова push-сервиса
func (m *Metrics) ObservePositionRequestDuration(ctx context.Context, seconds float64, result string) {
	if m == nil {
		return
	}
	m.PositionRequestDuration.Record(ctx, seconds, metric.WithAttributes(attribute.String(attrResult, result)))
}
