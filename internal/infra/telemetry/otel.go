package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationScope = "github.com/aquamarine66/tripgo-course"

// Config — настройки инициализации OpenTelemetry SDK.
type Config struct {
	ServiceName      string
	ServiceVersion   string
	ServiceNamespace string
	Environment      string
	OTLPEndpoint     string
	TracesSampler    string
}

// Providers содержит провайдеры трейсов и метрик и метод их корректного завершения.
type Providers struct {
	Tracer   trace.Tracer
	Meter    metric.Meter
	shutdown func(ctx context.Context) error
}

// Shutdown сбрасывает накопленные батчи трейсов и метрик и закрывает экспортеры.
func (p *Providers) Shutdown(ctx context.Context) error {
	if p == nil || p.shutdown == nil {
		return nil
	}
	return p.shutdown(ctx)
}

// Setup инициализирует TracerProvider, MeterProvider и W3C TraceContext propagator.
// Если OTLPEndpoint не задан, провайдеры работают без сетевого экспортера
// (спаны создаются и дают валидные trace_id/span_id для логов, а сервис не падает).
func Setup(ctx context.Context, cfg Config) (*Providers, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
			attribute.String("service.namespace", cfg.ServiceNamespace),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	closers := make([]func(context.Context) error, 0, 2)
	shutdownAll := func(sCtx context.Context) error {
		var result error
		for i := len(closers) - 1; i >= 0; i-- {
			result = errors.Join(result, closers[i](sCtx))
		}
		return result
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(selectSampler(cfg.TracesSampler)),
	}
	mpOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}

	if cfg.OTLPEndpoint != "" {
		traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithTimeout(2*time.Second))
		if err != nil {
			return nil, fmt.Errorf("create otlp trace exporter: %w", err)
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(traceExp, sdktrace.WithBatchTimeout(time.Second)))

		metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithTimeout(2*time.Second))
		if err != nil {
			return nil, fmt.Errorf("create otlp metric exporter: %w", err)
		}
		mpOpts = append(mpOpts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(5*time.Second)),
		))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	closers = append(closers, tp.Shutdown)

	mp := sdkmetric.NewMeterProvider(mpOpts...)
	closers = append(closers, mp.Shutdown)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Providers{
		Tracer:   tp.Tracer(instrumentationScope),
		Meter:    mp.Meter(instrumentationScope),
		shutdown: shutdownAll,
	}, nil
}

func selectSampler(name string) sdktrace.Sampler {
	switch name {
	case "always_off":
		return sdktrace.NeverSample()
	case "always_on":
		return sdktrace.AlwaysSample()
	default:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}
