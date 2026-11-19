package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aquamarine66/tripgo-course/internal/config"
	"github.com/aquamarine66/tripgo-course/internal/gateway/push"
	httpserver "github.com/aquamarine66/tripgo-course/internal/http"
	addposhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/addposition"
	createhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/create"
	finishhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/finish"
	gethandler "github.com/aquamarine66/tripgo-course/internal/http/trip/get"
	listposhandler "github.com/aquamarine66/tripgo-course/internal/http/trip/listpositions"
	"github.com/aquamarine66/tripgo-course/internal/infra/postgres"
	"github.com/aquamarine66/tripgo-course/internal/infra/postgres/transaction"
	"github.com/aquamarine66/tripgo-course/internal/infra/telemetry"
	triprepo "github.com/aquamarine66/tripgo-course/internal/repository/trip"
	addposuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/addposition"
	createuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/create"
	finishuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/finish"
	getuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/get"
	listposuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/listpositions"
	"github.com/aquamarine66/tripgo-course/internal/worker/positions"
)

// собирает и держит все зависимости сервиса в одном месте
type Container struct {
	cfg        *config.Config
	logger     *slog.Logger
	providers  *telemetry.Providers
	pool       *pgxpool.Pool
	pushClient *push.Client
	posPool    *positions.Pool
	httpSrv    *http.Server
}

// поднимаем телеметрию, пул бд, гейтвей пушей, воркер-пул, юзкейсы и http-сервер
func New(ctx context.Context, cfg *config.Config) (*Container, error) {
	logger := telemetry.NewLogger(cfg.OTelServiceName, cfg.LogLevel)

	providers, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:      cfg.OTelServiceName,
		ServiceVersion:   cfg.OTelServiceVersion,
		ServiceNamespace: cfg.OTelServiceNamespace,
		Environment:      cfg.OTelEnvironment,
		OTLPEndpoint:     cfg.OTelExporterEndpoint,
		OTLPProtocol:     cfg.OTelExporterProtocol,
		TracesSampler:    cfg.OTelTracesSampler,
		TracesSamplerArg: cfg.OTelTracesSamplerArg,
	})
	if err != nil {
		return nil, fmt.Errorf("setup telemetry: %w", err)
	}

	poolTracer := postgres.NewTracer(providers.TracerProvider, providers.MeterProvider)

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

	cleanup := func() {
		pool.Close()
		_ = providers.Shutdown(ctx)
	}

	txManager := transaction.NewManager(pool)
	repo := triprepo.New(pool, cfg.DBQueryTimeout)

	metrics, err := telemetry.NewMetrics(providers.Meter)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("init metrics: %w", err)
	}

	pushClient, err := push.New(push.Config{
		BaseURL:        cfg.PushHTTPURL,
		Timeout:        cfg.PushRequestTimeout,
		TracerProvider: providers.TracerProvider,
		Propagator:     providers.Propagator,
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("init push gateway: %w", err)
	}

	posPool := positions.New(positions.Config{
		CheckInterval:      cfg.PositionCheckInterval,
		StaleAfter:         cfg.PositionStaleAfter,
		Workers:            cfg.PositionWorkers,
		QueueSize:          cfg.PositionQueueSize,
		PushRequestTimeout: cfg.PushRequestTimeout,
	}, repo, pushClient, metrics, providers.Tracer, logger)

	createUseCase := createuc.New(repo, txManager, metrics, logger)
	getUseCase := getuc.New(repo)
	finishUseCase := finishuc.New(repo, txManager, metrics, logger)
	addPosUseCase := addposuc.New(repo, txManager, posPool, metrics)
	listPosUseCase := listposuc.New(repo)

	createHTTP, err := createhandler.New(createUseCase, logger)
	if err != nil {
		pushClient.Close()
		cleanup()
		return nil, fmt.Errorf("init create trip handler: %w", err)
	}
	addPosHTTP, err := addposhandler.New(addPosUseCase, logger)
	if err != nil {
		pushClient.Close()
		cleanup()
		return nil, fmt.Errorf("init add position handler: %w", err)
	}
	getHTTP := gethandler.New(getUseCase, logger)
	finishHTTP := finishhandler.New(finishUseCase, logger)
	listPosHTTP := listposhandler.New(listPosUseCase, logger)

	router := httpserver.NewRouter(httpserver.RouterConfig{
		Create:          createHTTP,
		Get:             getHTTP,
		Finish:          finishHTTP,
		AddPosition:     addPosHTTP,
		ListPositions:   listPosHTTP,
		DB:              pool,
		ReadyTimeout:    cfg.DBQueryTimeout,
		Logger:          logger,
		TracerProvider:  providers.TracerProvider,
		Propagator:      providers.Propagator,
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
		cfg:        cfg,
		logger:     logger,
		providers:  providers,
		pool:       pool,
		pushClient: pushClient,
		posPool:    posPool,
		httpSrv:    httpSrv,
	}, nil
}

// отдаём настроенный логгер сервиса
func (c *Container) Logger() *slog.Logger {
	return c.logger
}

// запускаем фоновый пул и http-сервер, а по сигналу останавливаем всё строго по порядку
func (c *Container) Run(ctx context.Context) error {
	baseCtx := context.WithoutCancel(ctx)

	// передаём контекст без автоотмены, чтобы сканер остановился строго после http-сервера
	c.posPool.Start(baseCtx)

	serverErrors := make(chan error, 1)
	go func() {
		c.logger.InfoContext(ctx, "http server started", "addr", c.cfg.HTTPAddr)
		serverErrors <- c.httpSrv.ListenAndServe()
	}()

	var runErr error
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server failed: %w", err)
		}
	case <-ctx.Done():
		c.logger.InfoContext(baseCtx, "shutdown signal received, stopping gracefully")
	}

	shutdownCtx, cancel := context.WithTimeout(baseCtx, c.cfg.ShutdownTimeout)
	defer cancel()

	// 1. перестаём принимать http-запросы и дожидаемся текущих
	if err := c.httpSrv.Shutdown(shutdownCtx); err != nil {
		c.logger.ErrorContext(shutdownCtx, "http server shutdown timed out, forcing close", "error", err)
		runErr = errors.Join(runErr, err, c.httpSrv.Close())
	}

	// 2. останавливаем сканер, чтобы новые задачи больше не появлялись в очереди
	c.posPool.StopScanner()

	// 3. даём воркерам доработать оставшиеся в очереди задачи в пределах SHUTDOWN_TIMEOUT
	if err := c.posPool.WaitWorkers(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, err)
	}

	// 4. закрываем исходящий http-клиент, экспортеры телеметрии и пул бд
	c.pushClient.Close()

	if err := c.providers.Shutdown(shutdownCtx); err != nil {
		c.logger.ErrorContext(shutdownCtx, "telemetry shutdown failed", "error", err)
		runErr = errors.Join(runErr, err)
	}

	c.pool.Close()
	c.logger.InfoContext(baseCtx, "service stopped")
	return runErr
}
