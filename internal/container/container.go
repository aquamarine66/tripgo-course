package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aquamarine66/tripgo-course/internal/config"
	httpserver "github.com/aquamarine66/tripgo-course/internal/http"
	createhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/create"
	finishhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/finish"
	gethandler "github.com/aquamarine66/tripgo-course/internal/http/trip/get"
	"github.com/aquamarine66/tripgo-course/internal/infra/postgres"
	"github.com/aquamarine66/tripgo-course/internal/infra/postgres/transaction"
	"github.com/aquamarine66/tripgo-course/internal/infra/telemetry"
	triprepo "github.com/aquamarine66/tripgo-course/internal/repository/trip"
	createuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/create"
	finishuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/finish"
	getuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/get"
)

// Container создаёт и связывает все зависимости сервиса в одном месте.
type Container struct {
	cfg       *config.Config
	logger    *slog.Logger
	providers *telemetry.Providers
	pool      *pgxpool.Pool
	httpSrv   *http.Server
}

// New инициализирует телеметрию, пул БД, репозитории, юзкейсы и HTTP-сервер.
func New(ctx context.Context, cfg *config.Config) (*Container, error) {
	logger := telemetry.NewLogger(cfg.OTelServiceName, cfg.LogLevel)

	providers, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:      cfg.OTelServiceName,
		ServiceVersion:   cfg.OTelServiceVersion,
		ServiceNamespace: cfg.OTelServiceNamespace,
		Environment:      cfg.OTelEnvironment,
		OTLPEndpoint:     cfg.OTelExporterEndpoint,
		TracesSampler:    cfg.OTelTracesSampler,
	})
	if err != nil {
		return nil, fmt.Errorf("setup telemetry: %w", err)
	}

	poolTracer, err := postgres.NewPoolTracer(providers.Tracer, providers.Meter)
	if err != nil {
		_ = providers.Shutdown(ctx)
		return nil, fmt.Errorf("create db pool tracer: %w", err)
	}

	pool, err := postgres.NewPool(ctx, postgres.Config{
		DSN:             cfg.DatabaseURL,
		MaxConns:        cfg.DBMaxConns,
		MinConns:        cfg.DBMinConns,
		MaxConnLifetime: cfg.DBMaxConnLifetime,
		ConnectTimeout:  cfg.DBConnectTimeout,
	}, poolTracer)
	if err != nil {
		_ = providers.Shutdown(ctx)
		return nil, fmt.Errorf("init postgres pool: %w", err)
	}

	txManager := transaction.NewManager(pool)
	repo := triprepo.New(pool, cfg.DBQueryTimeout)

	metrics, err := telemetry.NewMetrics(providers.Meter, repo)
	if err != nil {
		pool.Close()
		_ = providers.Shutdown(ctx)
		return nil, fmt.Errorf("init metrics: %w", err)
	}

	createUseCase := createuc.New(repo, txManager, metrics, logger)
	getUseCase := getuc.New(repo)
	finishUseCase := finishuc.New(repo, txManager, metrics, logger)

	createHTTP, err := createhandler.New(createUseCase, logger)
	if err != nil {
		pool.Close()
		_ = providers.Shutdown(ctx)
		return nil, fmt.Errorf("init create trip handler: %w", err)
	}
	getHTTP := gethandler.New(getUseCase, logger)
	finishHTTP := finishhandler.New(finishUseCase, logger)

	router := httpserver.NewRouter(httpserver.RouterConfig{
		Create:          createHTTP,
		Get:             getHTTP,
		Finish:          finishHTTP,
		DB:              pool,
		ReadyTimeout:    cfg.DBQueryTimeout,
		Logger:          logger,
		Tracer:          providers.Tracer,
		RequestDuration: metrics.HTTPRequestDuration,
		ActiveRequests:  metrics.HTTPActiveRequests,
	})

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	return &Container{
		cfg:       cfg,
		logger:    logger,
		providers: providers,
		pool:      pool,
		httpSrv:   httpSrv,
	}, nil
}

// Logger возвращает сконфигурированный логгер сервиса.
func (c *Container) Logger() *slog.Logger {
	return c.logger
}

// Run запускает HTTP-сервер, ждёт сигнала остановки и выполняет graceful shutdown
// в рамках общего бюджета SHUTDOWN_TIMEOUT.
func (c *Container) Run(ctx context.Context) error {
	serverErrors := make(chan error, 1)
	go func() {
		c.logger.InfoContext(ctx, "http server started", "addr", c.cfg.HTTPAddr)
		serverErrors <- c.httpSrv.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		c.logger.InfoContext(context.Background(), "shutdown signal received, stopping gracefully")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), c.cfg.ShutdownTimeout)
		defer cancel()

		var result error
		if err := c.httpSrv.Shutdown(shutdownCtx); err != nil {
			c.logger.ErrorContext(shutdownCtx, "graceful shutdown timed out, forcing close", "error", err)
			result = errors.Join(result, err, c.httpSrv.Close())
		}

		if err := c.providers.Shutdown(shutdownCtx); err != nil {
			c.logger.ErrorContext(shutdownCtx, "telemetry shutdown failed", "error", err)
			result = errors.Join(result, err)
		}

		c.pool.Close()
		c.logger.InfoContext(context.Background(), "service stopped")
		return result
	}
}
