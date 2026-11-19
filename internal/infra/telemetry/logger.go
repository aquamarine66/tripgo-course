package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// traceHandler добавляет trace_id и span_id из активного спана OpenTelemetry
// в каждую запись slog, а также постоянное поле service.
type traceHandler struct {
	inner slog.Handler
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, rec slog.Record) error {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		rec.AddAttrs(
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, rec)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{inner: h.inner.WithGroup(name)}
}

// NewLogger создаёт JSON-логгер slog, пишущий в stdout с уровнем из LOG_LEVEL,
// постоянным полем service и автоматической подстановкой trace_id и span_id из ctx.
func NewLogger(serviceName, levelStr string) *slog.Logger {
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(levelStr),
	}).WithAttrs([]slog.Attr{
		slog.String("service", serviceName),
	})

	return slog.New(&traceHandler{inner: base})
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
