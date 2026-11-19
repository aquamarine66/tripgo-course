package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
)

const unknownRoute = "unknown"

// обёртка показывает внешнему Recover, что панику уже залогировали внутри Tracing с trace_id
type loggedPanic struct {
	value any
}

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

// перехватываем панику на самом внешнем слое и отдаём клиенту 500 в формате problem+json
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := wrapResponseWriter(w)
			defer func() {
				if p := recover(); p != nil {
					val := p
					if lp, ok := p.(loggedPanic); ok {
						val = lp.value
					} else if logger != nil {
						logger.ErrorContext(r.Context(), "panic recovered",
							"panic", fmt.Sprint(val),
							"method", r.Method,
							"path", r.URL.Path,
							"stack", string(debug.Stack()),
						)
					}
					if !rec.wrote {
						domainerror.Write(rec, r, nil, fmt.Errorf("panic: %v", val))
					}
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

// инструментируем входящие http-запросы через otelhttp, переименовываем спан по шаблону маршрута
// и фиксируем панику в спане и логе до его закрытия, после чего пробрасываем её во внешний Recover
func Tracing(
	tp trace.TracerProvider,
	prop propagation.TextMapPropagator,
	logger *slog.Logger,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := wrapResponseWriter(w)
			defer func() {
				route := routePattern(r)
				span := trace.SpanFromContext(r.Context())
				if span.IsRecording() {
					span.SetName(r.Method + " " + route)
					span.SetAttributes(attribute.String("http.route", route))
				}

				if p := recover(); p != nil {
					if span.IsRecording() {
						span.RecordError(fmt.Errorf("panic: %v", p))
						span.SetStatus(codes.Error, "panic")
						span.SetAttributes(
							attribute.Int("http.response.status_code", http.StatusInternalServerError),
							attribute.String("error.type", "panic"),
						)
					}
					if logger != nil {
						logger.ErrorContext(r.Context(), "panic recovered",
							"panic", fmt.Sprint(p),
							"method", r.Method,
							"route", route,
							"path", r.URL.Path,
							"stack", string(debug.Stack()),
						)
					}
					panic(loggedPanic{value: p})
				}
			}()
			next.ServeHTTP(rec, r)
		})

		// метрики http пишет наш middleware.Metrics с точным шаблоном http.route, чтобы не было дублей
		opts := []otelhttp.Option{
			otelhttp.WithMeterProvider(noop.NewMeterProvider()),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + routePattern(r)
			}),
		}
		if tp != nil {
			opts = append(opts, otelhttp.WithTracerProvider(tp))
		}
		if prop != nil {
			opts = append(opts, otelhttp.WithPropagators(prop))
		}

		return otelhttp.NewHandler(inner, "", opts...)
	}
}

// пишем одну итоговую json-запись на каждый запрос: info для 2xx/4xx, error для 5xx
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

// записываем длительность запросов и счётчик активных запросов с шаблоном маршрута в http.route
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
		return unknownRoute
	}
	tctx := chi.NewRouteContext()
	if rctx.Routes.Match(tctx, r.Method, r.URL.Path) {
		if p := tctx.RoutePattern(); p != "" {
			return p
		}
	}
	return unknownRoute
}
