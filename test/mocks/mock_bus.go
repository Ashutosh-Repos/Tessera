package mocks

import (
	"context"
	"sync"
	"time"

	"github.com/distributed-transcoder/internal/infra"
)

// MockMessageBus implements infra.MessageBus for testing.
type MockMessageBus struct {
	mu             sync.Mutex
	Tasks          map[int][]infra.TaskMessage
	Events         map[string][][]byte
	DLQ            []infra.TaskMessage
	DLQHandlers    []func(msg infra.TaskMessage)
	UploadHandlers map[int]func(msg infra.TaskMessage)
	Err            error
}

func NewMockMessageBus() *MockMessageBus {
	return &MockMessageBus{
		Tasks:          make(map[int][]infra.TaskMessage),
		Events:         make(map[string][][]byte),
		UploadHandlers: make(map[int]func(msg infra.TaskMessage)),
	}
}

func (m *MockMessageBus) PublishTaskAsync(ctx context.Context, shard int, priority string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	msg := &MockTaskMessage{payload: payload}
	m.Tasks[shard] = append(m.Tasks[shard], msg)
	return nil
}

func (m *MockMessageBus) FlushPendingPublishes(ctx context.Context) error {
	return m.Err
}

func (m *MockMessageBus) PublishEvent(ctx context.Context, subject string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.Events[subject] = append(m.Events[subject], payload)
	return nil
}

func (m *MockMessageBus) PullTasks(ctx context.Context, shard int, batchSize int) ([]infra.TaskMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	tasks := m.Tasks[shard]
	if len(tasks) == 0 {
		return nil, nil
	}
	if len(tasks) <= batchSize {
		m.Tasks[shard] = nil
		return tasks, nil
	}
	res := tasks[:batchSize]
	m.Tasks[shard] = tasks[batchSize:]
	return res, nil
}

func (m *MockMessageBus) SubscribePartitionUploads(ctx context.Context, partitionID int, handler func(msg infra.TaskMessage)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UploadHandlers[partitionID] = handler
	return m.Err
}

func (m *MockMessageBus) SubscribeCompletionEvents(ctx context.Context, partitionID int, handler func(msg infra.TaskMessage)) error {
	return m.Err
}

func (m *MockMessageBus) SubscribeDLQ(ctx context.Context, handler func(msg infra.TaskMessage)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DLQHandlers = append(m.DLQHandlers, handler)
	return m.Err
}

func (m *MockMessageBus) GetDLQDepth() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.DLQ)), m.Err
}

func (m *MockMessageBus) InitEcosystem(shardCount int) error {
	return m.Err
}

func (m *MockMessageBus) Ping(ctx context.Context) error {
	return m.Err
}

func (m *MockMessageBus) Close() error {
	return nil
}

// MockTaskMessage implements infra.TaskMessage.
type MockTaskMessage struct {
	payload   []byte
	acked     bool
	nacked    bool
	nackDelay time.Duration
	numDeliv  int
}

func (m *MockTaskMessage) Data() []byte {
	return m.payload
}

func (m *MockTaskMessage) Ack() error {
	m.acked = true
	return nil
}

func (m *MockTaskMessage) Nak() error {
	m.nacked = true
	return nil
}

func (m *MockTaskMessage) NakWithDelay(delay time.Duration) error {
	m.nacked = true
	m.nackDelay = delay
	return nil
}

func (m *MockTaskMessage) InProgress() error {
	return nil
}

func (m *MockTaskMessage) Metadata() infra.TaskMessageMeta {
	return infra.TaskMessageMeta{
		NumDelivered: m.numDeliv,
		Timestamp:    time.Now().Unix(),
	}
}
