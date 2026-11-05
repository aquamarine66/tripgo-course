package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type ctxSpanKey struct{}

type queryFlight struct {
	span       trace.Span
	startedAt  time.Time
	operation  string
	collection string
}

var (
	reFromTable   = regexp.MustCompile(`(?i)\bFROM\s+([a-z_][a-z0-9_]*)`)
	reIntoTable   = regexp.MustCompile(`(?i)\bINTO\s+([a-z_][a-z0-9_]*)`)
	reUpdateTable = regexp.MustCompile(`(?i)\bUPDATE\s+([a-z_][a-z0-9_]*)`)
)

// Config — параметры пула соединений PostgreSQL.
type Config struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
}

// PoolTracer инструментирует все запросы пула pgx: создаёт спан на каждый
// запрос с атрибутами db.operation.name и db.collection.name и записывает
// гистограмму db.client.operation.duration.
type PoolTracer struct {
	tracer   trace.Tracer
	duration metric.Float64Histogram
}

// NewPoolTracer создаёт трейсер запросов pgx.
func NewPoolTracer(tracer trace.Tracer, meter metric.Meter) (*PoolTracer, error) {
	duration, err := meter.Float64Histogram(
		"db.client.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of database client operations in seconds"),
	)
	if err != nil {
		return nil, fmt.Errorf("create db.client.operation.duration histogram: %w", err)
	}
	return &PoolTracer{
		tracer:   tracer,
		duration: duration,
	}, nil
}

// NewPool создаёт и проверяет пул подключений pgx с подключённым трейсером.
func NewPool(ctx context.Context, cfg Config, queryTracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	if queryTracer != nil {
		poolCfg.ConnConfig.Tracer = queryTracer
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

// TraceQueryStart открывает спан перед выполнением SQL-запроса.
func (t *PoolTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	op, collection := parseSQLOperation(data.SQL)

	spanName := op
	if collection != "" {
		spanName = op + " " + collection
	}

	attrs := []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation.name", op),
	}
	if collection != "" {
		attrs = append(attrs, attribute.String("db.collection.name", collection))
	}

	ctx, span := t.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	return context.WithValue(ctx, ctxSpanKey{}, &queryFlight{
		span:       span,
		startedAt:  time.Now(),
		operation:  op,
		collection: collection,
	})
}

// TraceQueryEnd завершает спан и записывает метрику длительности запроса в БД.
func (t *PoolTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	flight, ok := ctx.Value(ctxSpanKey{}).(*queryFlight)
	if !ok || flight == nil {
		return
	}

	errType := ""
	if data.Err != nil {
		errType = fmt.Sprintf("%T", data.Err)
		flight.span.RecordError(data.Err)
		flight.span.SetStatus(codes.Error, data.Err.Error())
		flight.span.SetAttributes(attribute.String("error.type", errType))
	}
	flight.span.End()

	metricAttrs := []attribute.KeyValue{
		attribute.String("db.operation.name", flight.operation),
		attribute.String("db.collection.name", flight.collection),
		attribute.String("error.type", errType),
	}
	t.duration.Record(ctx, time.Since(flight.startedAt).Seconds(), metric.WithAttributes(metricAttrs...))
}

func parseSQLOperation(sqlText string) (operation, collection string) {
	trimmed := strings.TrimSpace(sqlText)
	if trimmed == "" {
		return "QUERY", ""
	}

	fields := strings.Fields(trimmed)
	operation = strings.ToUpper(fields[0])

	switch operation {
	case "SELECT", "DELETE":
		if m := reFromTable.FindStringSubmatch(trimmed); len(m) == 2 {
			collection = strings.ToLower(m[1])
		}
	case "INSERT":
		if m := reIntoTable.FindStringSubmatch(trimmed); len(m) == 2 {
			collection = strings.ToLower(m[1])
		}
	case "UPDATE":
		if m := reUpdateTable.FindStringSubmatch(trimmed); len(m) == 2 {
			collection = strings.ToLower(m[1])
		}
	}

	return operation, collection
}
