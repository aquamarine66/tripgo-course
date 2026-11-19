package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func wrapResponseWriter(w http.ResponseWriter) *statusRecorder {
	if rec, ok := w.(*statusRecorder); ok {
		return rec
	}
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// Recover ловит панику в цепочке обработчиков, логирует её со стектрейсом и
// отвечает 500 internal_error в формате application/problem+json.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := wrapResponseWriter(w)
			defer func() {
				if p := recover(); p != nil {
					if logger != nil {
						logger.ErrorContext(r.Context(), "panic recovered",
							"panic", fmt.Sprint(p),
							"method", r.Method,
							"path", r.URL.Path,
							"stack", string(debug.Stack()),
						)
					}
					if !rec.wrote {
						domainerror.Write(rec, r, nil, fmt.Errorf("panic: %v", p))
					}
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

// Tracing извлекает W3C TraceContext из заголовков, создаёт корневой спан
// входящего HTTP-запроса и переименовывает его в "{method} {http.route}" после роутинга chi.
func Tracing(tracer trace.Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(ctx, r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(attribute.String("http.request.method", r.Method)),
			)
			defer span.End()

			rec := wrapResponseWriter(w)
			next.ServeHTTP(rec, r.WithContext(ctx))

			route := routePattern(r)
			span.SetName(r.Method + " " + route)
			span.SetAttributes(
				attribute.String("http.route", route),
				attribute.Int("http.response.status_code", rec.status),
			)
			if rec.status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(rec.status))
				span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%d", rec.status)))
			}
		})
	}
}

// Logging пишет ровно одну JSON-запись на каждый HTTP-запрос после завершения ответа.
// Для 2xx и 4xx используется уровень Info, для 5xx — Error.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rec := wrapResponseWriter(w)

			next.ServeHTTP(rec, r)

			if logger == nil {
				return
			}

			route := routePattern(r)
			durationMs := time.Since(started).Milliseconds()
			attrs := []any{
				"method", r.Method,
				"route", route,
				"status", rec.status,
				"duration_ms", durationMs,
			}

			if rec.status >= http.StatusInternalServerError {
				logger.ErrorContext(r.Context(), "request handled", attrs...)
				return
			}
			logger.InfoContext(r.Context(), "request handled", attrs...)
		})
	}
}

// Metrics записывает метрики http.server.active_requests и http.server.request.duration
// с шаблоном маршрута в атрибуте http.route (без подстановки tripId).
func Metrics(
	requestDuration metric.Float64Histogram,
	activeRequests metric.Int64UpDownCounter,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rec := wrapResponseWriter(w)

			route := matchRoutePattern(r)
			activeAttrs := metric.WithAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("http.route", route),
			)
			if activeRequests != nil {
				activeRequests.Add(r.Context(), 1, activeAttrs)
				defer activeRequests.Add(r.Context(), -1, activeAttrs)
			}

			next.ServeHTTP(rec, r)

			if requestDuration != nil {
				finalRoute := routePattern(r)
				if finalRoute == "" {
					finalRoute = route
				}
				durAttrs := metric.WithAttributes(
					attribute.String("http.request.method", r.Method),
					attribute.String("http.route", finalRoute),
					attribute.Int("http.response.status_code", rec.status),
				)
				requestDuration.Record(r.Context(), time.Since(started).Seconds(), durAttrs)
			}
		})
	}
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return matchRoutePattern(r)
}

func matchRoutePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil || rctx.Routes == nil {
		return "unknown"
	}
	tctx := chi.NewRouteContext()
	if rctx.Routes.Match(tctx, r.Method, r.URL.Path) {
		if p := tctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unknown"
}
