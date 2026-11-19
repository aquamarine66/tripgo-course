package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/http/middleware"
)

func TestRecover_CatchesPanicAndReturnsProblemJSON(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	r := chi.NewRouter()
	r.Use(middleware.Recover(logger))
	r.Get("/boom", func(_ http.ResponseWriter, _ *http.Request) {
		panic("database exploded")
	})

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type=%q, want application/problem+json", ct)
	}

	var prob api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("unmarshal problem: %v", err)
	}
	if prob.Code != "internal_error" {
		t.Fatalf("prob.Code=%q, want internal_error", prob.Code)
	}
	if strings.Contains(rec.Body.String(), "database exploded") {
		t.Fatalf("panic message leaked to client: %s", rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "panic recovered") {
		t.Fatalf("expected panic to be logged, got: %s", logBuf.String())
	}
}

func TestTracingAndLogging_RouteTemplateAndTraceCorrelation(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	r := chi.NewRouter()
	r.Use(middleware.Recover(logger))
	r.Use(middleware.Tracing(tp.Tracer("test")))
	r.Use(middleware.Logging(logger))
	r.Get("/api/v1/trips/{tripId}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/1f0a9c62-4a1c-4f2e-9d33-2a4bb0f0b111", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != "GET /api/v1/trips/{tripId}" {
		t.Fatalf("span.Name=%q, want %q", spans[0].Name, "GET /api/v1/trips/{tripId}")
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, `"route":"/api/v1/trips/{tripId}"`) {
		t.Fatalf("expected route template in log, got: %s", logOutput)
	}
	if strings.Contains(logOutput, "1f0a9c62-4a1c-4f2e-9d33-2a4bb0f0b111") {
		t.Fatalf("concrete tripId should not appear in route field of request log: %s", logOutput)
	}
}
