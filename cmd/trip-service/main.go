package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/aquamarine66/tripgo-course/internal/config"
	"github.com/aquamarine66/tripgo-course/internal/container"
)

func main() {
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

func run() int {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := container.New(ctx, cfg)
	if err != nil {
		slog.Error("failed to initialize container", "error", err)
		return 1
	}

	if err := app.Run(ctx); err != nil {
		app.Logger().Error("service terminated with error", "error", err)
		return 1
	}

	return 0
}
