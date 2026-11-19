package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
	"github.com/aquamarine66/tripgo-course/internal/http/middleware"
	addposhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/addposition"
	createhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/create"
	finishhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/finish"
	gethandler "github.com/aquamarine66/tripgo-course/internal/http/trip/get"
	listposhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/listpositions"
	"github.com/aquamarine66/tripgo-course/internal/http/trip/response"
)

var _ api.ServerInterface = (*Server)(nil)

// проверка доступности postgres для readiness-пробы /ready
type Pinger interface {
	Ping(ctx context.Context) error
}

// связывает хендлеры со сгенерированным интерфейсом openapi
type Server struct {
	create        *createhandler.Handler
	get           *gethandler.Handler
	finish        *finishhandler.Handler
	addPosition   *addposhandler.Handler
	listPositions *listposhandler.Handler
	db            Pinger
	readyTimeout  time.Duration
}

// зависимости для сборки роутера chi и цепочки middleware
type RouterConfig struct {
	Create          *createhandler.Handler
	Get             *gethandler.Handler
	Finish          *finishhandler.Handler
	AddPosition     *addposhandler.Handler
	ListPositions   *listposhandler.Handler
	DB              Pinger
	ReadyTimeout    time.Duration
	Logger          *slog.Logger
	TracerProvider  trace.TracerProvider
	Propagator      propagation.TextMapPropagator
	RequestDuration metric.Float64Histogram
	ActiveRequests  metric.Int64UpDownCounter
}

// собираем chi-роутер с порядком middleware: Recover → Tracing → Logging → Metrics → роутер
func NewRouter(cfg RouterConfig) http.Handler {
	srv := &Server{
		create:        cfg.Create,
		get:           cfg.Get,
		finish:        cfg.Finish,
		addPosition:   cfg.AddPosition,
		listPositions: cfg.ListPositions,
		db:            cfg.DB,
		readyTimeout:  cfg.ReadyTimeout,
	}

	r := chi.NewRouter()
	r.Use(middleware.Recover(cfg.Logger))
	if cfg.TracerProvider != nil {
		r.Use(middleware.Tracing(cfg.TracerProvider, cfg.Propagator, cfg.Logger))
	}
	r.Use(middleware.Logging(cfg.Logger))
	if cfg.RequestDuration != nil || cfg.ActiveRequests != nil {
		r.Use(middleware.Metrics(cfg.RequestDuration, cfg.ActiveRequests))
	}
	r.NotFound(domainerror.NotFoundHandler)
	r.MethodNotAllowed(domainerror.MethodNotAllowedHandler)

	return api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: domainerror.ParamErrorHandler(cfg.Logger),
	})
}

// делегируем POST /api/v1/trips
func (s *Server) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	s.create.Handle(w, r)
}

// делегируем GET /api/v1/trips/{tripId}
func (s *Server) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.get.Handle(w, r, tripID)
}

// делегируем POST /api/v1/trips/{tripId}/finish
func (s *Server) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.finish.Handle(w, r, tripID)
}

// делегируем GET /api/v1/trips/{tripId}/positions
func (s *Server) ListTripPositions(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.listPositions.Handle(w, r, tripID)
}

// делегируем POST /api/v1/trips/{tripId}/positions
func (s *Server) CreateTripPosition(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.addPosition.Handle(w, r, tripID)
}

// liveness-проба (GET /health), в базу не ходит
func (s *Server) Health(w http.ResponseWriter, _ *http.Request) {
	response.WriteJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

// readiness-проба (GET /ready), проверяет доступность postgres
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
