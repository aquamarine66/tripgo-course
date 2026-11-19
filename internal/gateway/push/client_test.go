package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/aquamarine66/tripgo-course/internal/gateway/push"
)

func TestClient_RequestPosition_SuccessAndTracePropagation(t *testing.T) {
	t.Parallel()

	tripID := uuid.New()
	driverID := uuid.New()

	var (
		gotMethod      string
		gotPath        string
		gotTraceParent string
		gotBody        map[string]any
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotTraceParent = r.Header.Get("Traceparent")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"message_id":"m1","accepted_at":"2026-09-01T10:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	client, err := push.New(push.Config{
		BaseURL:        srv.URL,
		Timeout:        time.Second,
		TracerProvider: tp,
		Propagator:     propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("push.New(): %v", err)
	}
	t.Cleanup(client.Close)

	if err := client.RequestPosition(t.Context(), tripID, driverID); err != nil {
		t.Fatalf("RequestPosition() error: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/v1/push" {
		t.Fatalf("got %s %s, want POST /api/v1/push", gotMethod, gotPath)
	}
	if gotTraceParent == "" {
		t.Fatal("expected W3C traceparent header to be propagated")
	}
	if gotBody["kind"] != "REQUEST_POSITION" {
		t.Fatalf("kind=%v, want REQUEST_POSITION", gotBody["kind"])
	}
	if gotBody["recipient_id"] != driverID.String() {
		t.Fatalf("recipient_id=%v, want %s", gotBody["recipient_id"], driverID)
	}
	if gotBody["request_id"] == "" {
		t.Fatal("expected non-empty request_id")
	}
	data, ok := gotBody["data"].(map[string]any)
	if !ok || data["trip_id"] != tripID.String() {
		t.Fatalf("data=%v, want trip_id=%s", gotBody["data"], tripID)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "POST /api/v1/push" {
		t.Fatalf("expected 1 client span POST /api/v1/push, got %+v", spans)
	}
}

func TestClient_RequestPosition_Distinguishes4xx5xxAndTransportErrors(t *testing.T) {
	t.Parallel()

	t.Run("429 client error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		}))
		t.Cleanup(srv.Close)

		client, err := push.New(push.Config{BaseURL: srv.URL, Timeout: time.Second})
		if err != nil {
			t.Fatalf("push.New(): %v", err)
		}
		t.Cleanup(client.Close)

		err = client.RequestPosition(t.Context(), uuid.New(), uuid.New())
		if !errors.Is(err, push.ErrClientStatus) {
			t.Fatalf("expected ErrClientStatus, got %v", err)
		}
		var statusErr *push.StatusError
		if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTooManyRequests || statusErr.RetryAfter != 5*time.Second {
			t.Fatalf("unexpected status error: %+v", statusErr)
		}
	})

	t.Run("503 server error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unavailable"))
		}))
		t.Cleanup(srv.Close)

		client, err := push.New(push.Config{BaseURL: srv.URL, Timeout: time.Second})
		if err != nil {
			t.Fatalf("push.New(): %v", err)
		}
		t.Cleanup(client.Close)

		err = client.RequestPosition(t.Context(), uuid.New(), uuid.New())
		if !errors.Is(err, push.ErrServerStatus) {
			t.Fatalf("expected ErrServerStatus, got %v", err)
		}
	})

	t.Run("transport error on canceled context", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))
		srv.Close()

		client, err := push.New(push.Config{BaseURL: srv.URL, Timeout: 200 * time.Millisecond})
		if err != nil {
			t.Fatalf("push.New(): %v", err)
		}
		t.Cleanup(client.Close)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err = client.RequestPosition(ctx, uuid.New(), uuid.New())
		if !errors.Is(err, push.ErrTransport) {
			t.Fatalf("expected ErrTransport, got %v", err)
		}
	})
}
