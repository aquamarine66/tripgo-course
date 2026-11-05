package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
	"github.com/aquamarine66/tripgo-course/internal/http/middleware"
	createhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/create"
	finishhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/finish"
	gethandler "github.com/aquamarine66/tripgo-course/internal/http/trip/get"
	"github.com/aquamarine66/tripgo-course/internal/http/trip/response"
)

var _ api.ServerInterface = (*Server)(nil)

// Pinger проверяет доступность PostgreSQL для readiness-пробы /ready.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Server объединяет ручки пакетов http/trip/* и реализует сгенерированный api.ServerInterface.
type Server struct {
	create       *createhandler.Handler
	get          *gethandler.Handler
	finish       *finishhandler.Handler
	db           Pinger
	readyTimeout time.Duration
}

// RouterConfig задаёт зависимости для сборки HTTP-роутера chi и цепочки middleware.
type RouterConfig struct {
	Create          *createhandler.Handler
	Get             *gethandler.Handler
	Finish          *finishhandler.Handler
	DB              Pinger
	ReadyTimeout    time.Duration
	Logger          *slog.Logger
	Tracer          trace.Tracer
	RequestDuration metric.Float64Histogram
	ActiveRequests  metric.Int64UpDownCounter
}

// NewRouter собирает роутер chi со строго заданным порядком middleware:
// Recover → Tracing → Logging → Metrics → роутер.
func NewRouter(cfg RouterConfig) http.Handler {
	srv := &Server{
		create:       cfg.Create,
		get:          cfg.Get,
		finish:       cfg.Finish,
		db:           cfg.DB,
		readyTimeout: cfg.ReadyTimeout,
	}

	r := chi.NewRouter()
	r.Use(middleware.Recover(cfg.Logger))
	if cfg.Tracer != nil {
		r.Use(middleware.Tracing(cfg.Tracer))
	}
	r.Use(middleware.Logging(cfg.Logger))
	if cfg.RequestDuration != nil || cfg.ActiveRequests != nil {
		r.Use(middleware.Metrics(cfg.RequestDuration, cfg.ActiveRequests))
	}

	return api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: domainerror.ParamErrorHandler(cfg.Logger),
	})
}

// CreateTrip делегирует POST /api/v1/trips пакету internal/http/trip/create.
func (s *Server) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	s.create.Handle(w, r)
}

// GetTrip делегирует GET /api/v1/trips/{tripId} пакету internal/http/trip/get.
func (s *Server) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.get.Handle(w, r, tripID)
}

// FinishTrip делегирует POST /api/v1/trips/{tripId}/finish пакету internal/http/trip/finish.
func (s *Server) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.finish.Handle(w, r, tripID)
}

// ListTripPositions — заглушка для лабораторной работы 3.
func (s *Server) ListTripPositions(w http.ResponseWriter, r *http.Request, _ api.TripId) {
	domainerror.WriteNotImplemented(w, r, "Trip positions are not implemented yet")
}

// CreateTripPosition — заглушка для лабораторной работы 3.
func (s *Server) CreateTripPosition(w http.ResponseWriter, r *http.Request, _ api.TripId) {
	domainerror.WriteNotImplemented(w, r, "Trip positions are not implemented yet")
}

// Health — liveness-проба (GET /health), не обращается к БД.
func (s *Server) Health(w http.ResponseWriter, _ *http.Request) {
	response.WriteJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

// Ready — readiness-проба (GET /ready), проверяет доступность PostgreSQL.
func (s *Server) Ready(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}

	timeout := s.readyTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}

	response.WriteJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
