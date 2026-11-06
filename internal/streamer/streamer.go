package streamer

import (
	"context"
	"log/slog"
	"time"

	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/queue"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/telemetry"
)

type Streamer struct {
	Publisher queue.Publisher
	Source    *telemetry.CSVSource
	SourceID  string
	Interval  time.Duration
	Logger    *slog.Logger
}

func (s *Streamer) Run(ctx context.Context) error {
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.Publisher == nil || s.Source == nil || s.SourceID == "" {
		return errInvalidConfig
	}
	if s.Interval <= 0 {
		return errInvalidConfig
	}
	for {
		published, err := s.Publisher.PublishNext(ctx, s.SourceID, s.Source.HeaderOffset(), func(offset int64) ([]byte, int64, bool, error) {
			return s.Source.ReadAt(offset, time.Now().UTC())
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !published {
			s.Logger.Warn("CSV source contains no telemetry rows", "source_id", s.SourceID)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.Interval):
		}
	}
}

var errInvalidConfig = &streamerError{"publisher, CSV source, source ID and positive interval are required"}

type streamerError struct{ message string }

func (e *streamerError) Error() string { return e.message }
