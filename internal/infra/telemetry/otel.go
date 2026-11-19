package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationScope = "github.com/aquamarine66/tripgo-course"
	protocolHTTPProtobuf = "http/protobuf"
	protocolGRPC         = "grpc"
	exportTimeout        = 2 * time.Second
)

// настройки инициализации opentelemetry sdk
type Config struct {
	ServiceName      string
	ServiceVersion   string
	ServiceNamespace string
	Environment      string
	OTLPEndpoint     string
	OTLPProtocol     string
	TracesSampler    string
	TracesSamplerArg float64
}

// собранные провайдеры, пропагатор и закрывалка для передачи через di без глобального состояния
type Providers struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
	Tracer         trace.Tracer
	Meter          metric.Meter
	shutdown       func(ctx context.Context) error
}

// сбрасываем буферы трейсов и метрик и закрываем экспортеры
func (p *Providers) Shutdown(ctx context.Context) error {
	if p == nil || p.shutdown == nil {
		return nil
	}
	return p.shutdown(ctx)
}

// поднимаем провайдеры трейсов, метрик и w3c-пропагатор; если endpoint пустой, работаем локально без сети
func Setup(ctx context.Context, cfg Config) (*Providers, error) {
	protocol := cfg.OTLPProtocol
	if protocol == "" {
		protocol = protocolHTTPProtobuf
	}
	if protocol != protocolHTTPProtobuf && protocol != protocolGRPC {
		return nil, fmt.Errorf("unsupported otlp protocol: %q", protocol)
	}

	sampler, err := selectSampler(cfg.TracesSampler, cfg.TracesSamplerArg)
	if err != nil {
		return nil, err
	}

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
		sdktrace.WithSampler(sampler),
	}
	mpOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}

	if cfg.OTLPEndpoint != "" {
		traceExp, metricExp, err := buildExporters(ctx, protocol)
		if err != nil {
			return nil, err
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(traceExp, sdktrace.WithBatchTimeout(time.Second)))
		mpOpts = append(mpOpts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(5*time.Second)),
		))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	closers = append(closers, tp.Shutdown)

	mp := sdkmetric.NewMeterProvider(mpOpts...)
	closers = append(closers, mp.Shutdown)

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	return &Providers{
		TracerProvider: tp,
		MeterProvider:  mp,
		Propagator:     propagator,
		Tracer:         tp.Tracer(instrumentationScope),
		Meter:          mp.Meter(instrumentationScope),
		shutdown:       shutdownAll,
	}, nil
}

func buildExporters(ctx context.Context, protocol string) (sdktrace.SpanExporter, sdkmetric.Exporter, error) {
	switch protocol {
	case protocolHTTPProtobuf:
		traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithTimeout(exportTimeout))
		if err != nil {
			return nil, nil, fmt.Errorf("create otlp http trace exporter: %w", err)
		}
		metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithTimeout(exportTimeout))
		if err != nil {
			_ = traceExp.Shutdown(ctx)
			return nil, nil, fmt.Errorf("create otlp http metric exporter: %w", err)
		}
		return traceExp, metricExp, nil

	case protocolGRPC:
		traceExp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithTimeout(exportTimeout))
		if err != nil {
			return nil, nil, fmt.Errorf("create otlp grpc trace exporter: %w", err)
		}
		metricExp, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithTimeout(exportTimeout))
		if err != nil {
			_ = traceExp.Shutdown(ctx)
			return nil, nil, fmt.Errorf("create otlp grpc metric exporter: %w", err)
		}
		return traceExp, metricExp, nil

	default:
		return nil, nil, fmt.Errorf("unsupported otlp protocol: %q", protocol)
	}
}

func selectSampler(name string, arg float64) (sdktrace.Sampler, error) {
	switch name {
	case "", "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(arg)), nil
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(arg), nil
	default:
		return nil, fmt.Errorf("unsupported traces sampler: %q", name)
	}
}
