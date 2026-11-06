package queue

import (
	"context"
	"time"
)

type Message struct {
	ID        string
	Payload   []byte
	Attempts  int
	CreatedAt time.Time
}

type OffsetReader func(offset int64) (payload []byte, nextOffset int64, eof bool, err error)

type Publisher interface {
	PublishNext(context.Context, string, int64, OffsetReader) (bool, error)
}

type Consumer interface {
	Claim(context.Context, string, int, time.Duration) ([]Message, error)
	Ack(context.Context, string, string) error
	Nack(context.Context, string, string, time.Duration, int, string) error
}
