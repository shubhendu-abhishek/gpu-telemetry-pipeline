package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/collector"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/config"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Collector stopped with error", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	service := &collector.Collector{Queue: store, Sink: store, ID: hostname(), Logger: logger, Config: collector.Config{Workers: cfg.CollectorWorkers, BatchSize: cfg.CollectorBatchSize, PollInterval: cfg.CollectorPollInterval, LeaseDuration: cfg.QueueLeaseDuration, MaxAttempts: cfg.QueueMaxAttempts}}
	return service.Run(ctx)
}
func hostname() string {
	value, err := os.Hostname()
	if err != nil || value == "" {
		return "collector"
	}
	return value
}
