package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL           string
	HTTPPort              string
	CSVPath               string
	SourceID              string
	StreamerInterval      time.Duration
	CollectorWorkers      int
	CollectorBatchSize    int
	CollectorPollInterval time.Duration
	QueueLeaseDuration    time.Duration
	QueueMaxAttempts      int
	LogLevel              string
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://gpu_telemetry:gpu_telemetry_dev@localhost:5432/gpu_telemetry?sslmode=disable"),
		HTTPPort:    getEnv("HTTP_PORT", "8080"),
		CSVPath:     getEnv("TELEMETRY_CSV_PATH", "./data/dcgm_metrics_20250718_134233.csv"),
		SourceID:    getEnv("TELEMETRY_SOURCE_ID", "dcgm-metrics-sample"),
		LogLevel:    strings.ToLower(getEnv("LOG_LEVEL", "info")),
	}
	var err error
	if cfg.StreamerInterval, err = durationEnv("STREAMER_INTERVAL", 500*time.Millisecond); err != nil { return Config{}, err }
	if cfg.CollectorPollInterval, err = durationEnv("COLLECTOR_POLL_INTERVAL", 500*time.Millisecond); err != nil { return Config{}, err }
	if cfg.QueueLeaseDuration, err = durationEnv("QUEUE_LEASE_DURATION", 60*time.Second); err != nil { return Config{}, err }
	if cfg.CollectorWorkers, err = intEnv("COLLECTOR_WORKERS", 4); err != nil { return Config{}, err }
	if cfg.CollectorBatchSize, err = intEnv("COLLECTOR_BATCH_SIZE", 25); err != nil { return Config{}, err }
	if cfg.QueueMaxAttempts, err = intEnv("QUEUE_MAX_ATTEMPTS", 10); err != nil { return Config{}, err }
	if err = cfg.Validate(); err != nil { return Config{}, err }
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" { return errors.New("DATABASE_URL must not be empty") }
	port, err := strconv.Atoi(c.HTTPPort)
	if err != nil || port < 1 || port > 65535 { return fmt.Errorf("HTTP_PORT must be between 1 and 65535: %q", c.HTTPPort) }
	if strings.TrimSpace(c.CSVPath) == "" { return errors.New("TELEMETRY_CSV_PATH must not be empty") }
	if strings.TrimSpace(c.SourceID) == "" { return errors.New("TELEMETRY_SOURCE_ID must not be empty") }
	if c.StreamerInterval <= 0 || c.CollectorPollInterval <= 0 || c.QueueLeaseDuration <= 0 { return errors.New("intervals and lease duration must be positive") }
	if c.CollectorWorkers < 1 || c.CollectorWorkers > 10 { return errors.New("COLLECTOR_WORKERS must be between 1 and 10") }
	if c.CollectorBatchSize < 1 || c.CollectorBatchSize > 500 { return errors.New("COLLECTOR_BATCH_SIZE must be between 1 and 500") }
	if c.QueueMaxAttempts < 1 || c.QueueMaxAttempts > 100 { return errors.New("QUEUE_MAX_ATTEMPTS must be between 1 and 100") }
	return nil
}
func getEnv(key, fallback string) string { if value, ok := os.LookupEnv(key); ok { return value }; return fallback }
func durationEnv(key string, fallback time.Duration) (time.Duration, error) { value, ok := os.LookupEnv(key); if !ok { return fallback, nil }; parsed, err := time.ParseDuration(value); if err != nil { return 0, fmt.Errorf("%s: %w", key, err) }; return parsed, nil }
func intEnv(key string, fallback int) (int, error) { value, ok := os.LookupEnv(key); if !ok { return fallback, nil }; parsed, err := strconv.Atoi(value); if err != nil { return 0, fmt.Errorf("%s must be an integer: %w", key, err) }; return parsed, nil }
