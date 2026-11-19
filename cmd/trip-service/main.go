package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/aquamarine66/tripgo-course/internal/api"
	"github.com/aquamarine66/tripgo-course/internal/config"
	"github.com/aquamarine66/tripgo-course/internal/repository"
	"github.com/aquamarine66/tripgo-course/internal/service"
)

func main() {
	os.Exit(run())
}

func run() int {
	// подгружаем локальный .env, если он лежит рядом
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		return 1
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	slog.Info("initializing trip-service", "http_addr", cfg.HTTPAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// настраиваем пул подключений и ограничиваем время коннекта к базе
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		slog.Error("invalid database url", "error", err)
		return 1
	}

	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolCfg.ConnConfig.ConnectTimeout = cfg.DBConnectTimeout
	poolCfg.PingTimeout = cfg.DBConnectTimeout

	// один общий контекст с таймаутом на поднятие пула и стартовый ping, чтобы сервис не зависал при недоступной базе
	connectCtx, connectCancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	defer connectCancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		slog.Error("failed to create db pool", "error", err)
		return 1
	}
	defer pool.Close()

	if err := pool.Ping(connectCtx); err != nil {
		slog.Error("failed to ping database", "error", err)
		return 1
	}
	slog.Info("connected to postgresql pool")

	// собираем зависимости
	txManager := repository.NewTxManager(pool, cfg.DBQueryTimeout)
	tripRepo := repository.NewTripRepository(pool, cfg.DBQueryTimeout)
	tripService := service.NewTripService(tripRepo, txManager)
	tripHandler, err := api.NewTripHandler(tripService, pool, cfg.DBQueryTimeout)
	if err != nil {
		slog.Error("failed to create http handler", "error", err)
		return 1
	}

	// настраиваем роутер и мидлвари, паники и неизвестные пути тоже отдаём в problem+json
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(api.RecoverMiddleware)
	r.Use(middleware.Timeout(cfg.DBQueryTimeout + 2*time.Second))
	r.NotFound(api.NotFoundHandler)
	r.MethodNotAllowed(api.MethodNotAllowedHandler)

	// вешаем сгенерированные роуты с общим обработчиком ошибок параметров
	handler := api.HandlerWithOptions(tripHandler, api.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: api.ParamErrorHandler,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("http server started", "addr", cfg.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()

	// ждём либо падения сервера, либо сигнала на остановку
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "error", err)
			return 1
		}
	case <-ctx.Done():
		slog.Info("shutdown signal caught, terminating gracefully...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown timed out, forcing close", "error", err)
			_ = server.Close()
		}
		slog.Info("service exited cleanly")
	}

	return 0
}

func parseLogLevel(level string) slog.Level {
	switch level {
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
