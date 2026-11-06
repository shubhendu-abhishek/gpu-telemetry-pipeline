package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/queue"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/telemetry"
)

type Queue interface { queue.Consumer }
type Sink interface { SaveTelemetry(context.Context, string, telemetry.Record) error }
type Config struct { Workers int; BatchSize int; PollInterval time.Duration; LeaseDuration time.Duration; MaxAttempts int }
type Collector struct { Queue Queue; Sink Sink; Config Config; Logger *slog.Logger; ID string }

func (c *Collector) Run(ctx context.Context) error {
	if c.Logger == nil { c.Logger = slog.Default() }
	if c.Queue == nil || c.Sink == nil || c.ID == "" || c.Config.Workers < 1 || c.Config.Workers > 10 || c.Config.BatchSize < 1 || c.Config.PollInterval <= 0 || c.Config.LeaseDuration <= 0 || c.Config.MaxAttempts < 1 { return fmt.Errorf("invalid collector configuration") }
	var wg sync.WaitGroup
	for i := 0; i < c.Config.Workers; i++ { wg.Add(1); go func(worker int) { defer wg.Done(); c.worker(ctx, worker) }(i) }
	<-ctx.Done(); wg.Wait(); return nil
}
func (c *Collector) worker(ctx context.Context, worker int) {
	consumerID := fmt.Sprintf("%s-%d", c.ID, worker)
	for ctx.Err() == nil {
		messages, err := c.Queue.Claim(ctx, consumerID, c.Config.BatchSize, c.Config.LeaseDuration)
		if err != nil { if ctx.Err() != nil { return }; c.Logger.Error("queue claim failed", "consumer", consumerID, "error", err); sleep(ctx, c.Config.PollInterval); continue }
		if len(messages) == 0 { sleep(ctx, c.Config.PollInterval); continue }
		for _, message := range messages { if ctx.Err() != nil { return }; c.process(ctx, consumerID, message) }
	}
}
func (c *Collector) process(ctx context.Context, consumerID string, message queue.Message) {
	var record telemetry.Record
	if err := json.Unmarshal(message.Payload, &record); err != nil { c.nack(ctx, consumerID, message, fmt.Errorf("decode telemetry payload: %w", err)); return }
	if record.GPUUUID == "" || record.MetricName == "" || record.StreamedAt.IsZero() { c.nack(ctx, consumerID, message, fmt.Errorf("payload is missing required telemetry fields")); return }
	record.ProcessedAt = time.Now().UTC()
	if err := c.Sink.SaveTelemetry(ctx, message.ID, record); err != nil { c.nack(ctx, consumerID, message, err); return }
	if err := c.Queue.Ack(ctx, message.ID, consumerID); err != nil { c.Logger.Error("queue acknowledgement failed; message may be redelivered", "message_id", message.ID, "error", err); return }
}
func (c *Collector) nack(ctx context.Context, consumerID string, message queue.Message, cause error) {
	delay := time.Second * time.Duration(math.Min(math.Pow(2, float64(max(message.Attempts-1, 0))), 60))
	if err := c.Queue.Nack(ctx, message.ID, consumerID, delay, c.Config.MaxAttempts, cause.Error()); err != nil { c.Logger.Error("queue nack failed", "message_id", message.ID, "cause", cause, "error", err) }
}
func sleep(ctx context.Context, duration time.Duration) { timer := time.NewTimer(duration); defer timer.Stop(); select { case <-ctx.Done(): case <-timer.C: } }
