package config

import (
	"os"
	"testing"
	"time"
)

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"DATABASE_URL", "HTTP_PORT", "TELEMETRY_CSV_PATH", "TELEMETRY_SOURCE_ID", "STREAMER_INTERVAL", "COLLECTOR_WORKERS", "COLLECTOR_BATCH_SIZE", "COLLECTOR_POLL_INTERVAL", "QUEUE_LEASE_DURATION", "QUEUE_MAX_ATTEMPTS", "LOG_LEVEL"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
}
func TestLoadDefaults(t *testing.T) {
	cleanEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CollectorWorkers != 4 || cfg.CollectorBatchSize != 25 || cfg.StreamerInterval != 500*time.Millisecond {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
func TestLoadInvalidDuration(t *testing.T) {
	cleanEnv(t)
	t.Setenv("STREAMER_INTERVAL", "tomorrow")
	if _, err := Load(); err == nil {
		t.Fatal("expected duration error")
	}
}
func TestValidateRejectsInvalidRanges(t *testing.T) {
	cfg := Config{DatabaseURL: "db", HTTPPort: "8080", CSVPath: "data.csv", SourceID: "source", StreamerInterval: time.Second, CollectorPollInterval: time.Second, QueueLeaseDuration: time.Minute, CollectorWorkers: 11, CollectorBatchSize: 1, QueueMaxAttempts: 1}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected worker limit error")
	}
}
func TestValidateRejectsBadPort(t *testing.T) {
	cfg := Config{DatabaseURL: "db", HTTPPort: "70000", CSVPath: "data.csv", SourceID: "source", StreamerInterval: time.Second, CollectorPollInterval: time.Second, QueueLeaseDuration: time.Minute, CollectorWorkers: 1, CollectorBatchSize: 1, QueueMaxAttempts: 1}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected port error")
	}
}
func TestLoadValidOverrides(t *testing.T) {
	cleanEnv(t)
	t.Setenv("HTTP_PORT", "9090")
	t.Setenv("STREAMER_INTERVAL", "2s")
	t.Setenv("COLLECTOR_WORKERS", "8")
	t.Setenv("COLLECTOR_BATCH_SIZE", "50")
	t.Setenv("QUEUE_MAX_ATTEMPTS", "5")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPPort != "9090" || cfg.StreamerInterval != 2*time.Second || cfg.CollectorWorkers != 8 || cfg.CollectorBatchSize != 50 || cfg.QueueMaxAttempts != 5 {
		t.Fatalf("overrides not loaded: %+v", cfg)
	}
}
func TestLoadInvalidIntegerOverrides(t *testing.T) {
	for _, key := range []string{"COLLECTOR_WORKERS", "COLLECTOR_BATCH_SIZE", "QUEUE_MAX_ATTEMPTS"} {
		t.Run(key, func(t *testing.T) {
			cleanEnv(t)
			t.Setenv(key, "not-a-number")
			if _, err := Load(); err == nil {
				t.Fatalf("expected %s parsing error", key)
			}
		})
	}
}
func TestValidateAllRequiredAndRangeChecks(t *testing.T) {
	base := Config{DatabaseURL: "postgres://localhost/db", HTTPPort: "8080", CSVPath: "data.csv", SourceID: "source", StreamerInterval: time.Second, CollectorPollInterval: time.Second, QueueLeaseDuration: time.Minute, CollectorWorkers: 2, CollectorBatchSize: 10, QueueMaxAttempts: 5}
	cases := []struct {
		name   string
		mutate func(*Config)
	}{{"database url", func(c *Config) { c.DatabaseURL = "" }}, {"csv path", func(c *Config) { c.CSVPath = " " }}, {"source id", func(c *Config) { c.SourceID = "" }}, {"zero streamer interval", func(c *Config) { c.StreamerInterval = 0 }}, {"zero poll interval", func(c *Config) { c.CollectorPollInterval = 0 }}, {"zero lease duration", func(c *Config) { c.QueueLeaseDuration = 0 }}, {"zero workers", func(c *Config) { c.CollectorWorkers = 0 }}, {"zero batch size", func(c *Config) { c.CollectorBatchSize = 0 }}, {"zero max attempts", func(c *Config) { c.QueueMaxAttempts = 0 }}, {"invalid port", func(c *Config) { c.HTTPPort = "abc" }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}
