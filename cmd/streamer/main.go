package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/config"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/postgres"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/streamer"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Streamer stopped with error", "error", err)
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
	source, err := telemetry.OpenCSVSource(cfg.CSVPath)
	if err != nil {
		return err
	}
	defer source.Close()
	logger.Info("CSV source opened", "path", source.Path(), "header_offset", source.HeaderOffset(), "source_id", cfg.SourceID)
	return (&streamer.Streamer{Publisher: store, Source: source, SourceID: cfg.SourceID, Interval: cfg.StreamerInterval, Logger: logger}).Run(ctx)
}
