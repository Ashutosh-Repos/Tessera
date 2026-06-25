package infra

import (
	"context"
	"time"
)

// MessageBus abstracts NATS JetStream operations.
type MessageBus interface {
	// Publishing (Coordinator → Workers)
	PublishTaskAsync(ctx context.Context, shard int, priority string, payload []byte) error
	FlushPendingPublishes(ctx context.Context) error // blocks until all PublishAsync futures resolve
	PublishEvent(ctx context.Context, subject string, payload []byte) error

	// Consuming (Workers pull tasks)
	PullTasks(ctx context.Context, shard int, batchSize int) ([]TaskMessage, error)

	// Partition-scoped events
	SubscribePartitionUploads(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error
	SubscribeCompletionEvents(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error

	// DLQ
	SubscribeDLQ(ctx context.Context, handler func(msg TaskMessage)) error
	GetDLQDepth() (int64, error)

	// Ecosystem initialization
	InitEcosystem(shardCount int) error

	// Health
	Ping(ctx context.Context) error

	Close() error
}

// TaskMessage wraps a NATS JetStream message.
type TaskMessage interface {
	Data() []byte
	Ack() error
	Nak() error        // negative ack → immediate redelivery
	NakWithDelay(delay time.Duration) error
	InProgress() error // extend AckWait deadline
	Metadata() TaskMessageMeta
}

type TaskMessageMeta struct {
	NumDelivered int
	Timestamp    int64
}
