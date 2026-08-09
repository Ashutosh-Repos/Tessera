package mocks

import (
	"context"
	"sync"

	"github.com/distributed-transcoder/internal/infra"
)

// MockCoordination implements infra.Coordination for testing.
type MockCoordination struct {
	mu           sync.Mutex
	Coordinators []string
	Locks        map[string]string
	Err          error
}

func NewMockCoordination() *MockCoordination {
	return &MockCoordination{
		Locks: make(map[string]string),
	}
}

func (m *MockCoordination) Register(ctx context.Context, nodeID string, leaseTTLSec int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return 0, m.Err
	}
	m.Coordinators = append(m.Coordinators, nodeID)
	return 1001, nil
}

func (m *MockCoordination) Deregister(ctx context.Context, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	for i, c := range m.Coordinators {
		if c == nodeID {
			m.Coordinators = append(m.Coordinators[:i], m.Coordinators[i+1:]...)
			break
		}
	}
	return nil
}

func (m *MockCoordination) WatchCoordinators(ctx context.Context) (<-chan infra.CoordinatorEvent, error) {
	ch := make(chan infra.CoordinatorEvent, 10)
	return ch, m.Err
}

func (m *MockCoordination) GetCoordinators(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	return append([]string(nil), m.Coordinators...), nil
}

func (m *MockCoordination) AcquireSlicingLock(ctx context.Context, jobID string, ownerID string, ttlSec int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return false, m.Err
	}
	if existing, held := m.Locks[jobID]; held && existing != ownerID {
		return false, nil
	}
	m.Locks[jobID] = ownerID
	return true, nil
}

func (m *MockCoordination) ReleaseSlicingLock(ctx context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Locks, jobID)
	return m.Err
}

func (m *MockCoordination) KeepAliveLock(ctx context.Context, leaseID int64) error {
	return m.Err
}

func (m *MockCoordination) Ping(ctx context.Context) error {
	return m.Err
}

func (m *MockCoordination) Close() error {
	return nil
}
