package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
)

// MockStateStore implements infra.StateStore for testing.
type MockStateStore struct {
	mu           sync.RWMutex
	Statuses     map[string]map[string]string
	ActiveJobs   map[int][]string
	Manifests    map[string][]byte
	Bitmaps      map[string]map[int]bool
	Durations    map[string]map[string]string
	TaskDoneMap  map[string]bool
	RateLimits   map[string]int64
	ProgressLogs []models.ProgressUpdate
	Err          error
}

func NewMockStateStore() *MockStateStore {
	return &MockStateStore{
		Statuses:    make(map[string]map[string]string),
		ActiveJobs:  make(map[int][]string),
		Manifests:   make(map[string][]byte),
		Bitmaps:     make(map[string]map[int]bool),
		Durations:   make(map[string]map[string]string),
		TaskDoneMap: make(map[string]bool),
		RateLimits:  make(map[string]int64),
	}
}

func (m *MockStateStore) SetJobStatus(ctx context.Context, jobID string, status map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if m.Statuses[jobID] == nil {
		m.Statuses[jobID] = make(map[string]string)
	}
	for k, v := range status {
		m.Statuses[jobID][k] = fmt.Sprintf("%v", v)
	}
	return nil
}

func (m *MockStateStore) GetJobStatus(ctx context.Context, jobID string) (map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return nil, m.Err
	}
	st, ok := m.Statuses[jobID]
	if !ok {
		return nil, nil
	}
	res := make(map[string]string)
	for k, v := range st {
		res[k] = v
	}
	return res, nil
}

func (m *MockStateStore) IncrJobCompleted(ctx context.Context, jobID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return 0, m.Err
	}
	return 1, nil
}

func (m *MockStateStore) SetBit(ctx context.Context, jobID string, bitIdx int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if m.Bitmaps[jobID] == nil {
		m.Bitmaps[jobID] = make(map[int]bool)
	}
	m.Bitmaps[jobID][bitIdx] = true
	return nil
}

func (m *MockStateStore) BitCount(ctx context.Context, jobID string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return 0, m.Err
	}
	return int64(len(m.Bitmaps[jobID])), nil
}

func (m *MockStateStore) TaskExists(ctx context.Context, jobID string, segment int, res string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return false, m.Err
	}
	key := fmt.Sprintf("%s:%d:%s", jobID, segment, res)
	return m.TaskDoneMap[key], nil
}

func (m *MockStateStore) SetTaskDone(ctx context.Context, jobID string, segment int, res string, ttl int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	key := fmt.Sprintf("%s:%d:%s", jobID, segment, res)
	m.TaskDoneMap[key] = true
	return nil
}

func (m *MockStateStore) SetSegmentDuration(ctx context.Context, jobID string, segRes string, duration string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if m.Durations[jobID] == nil {
		m.Durations[jobID] = make(map[string]string)
	}
	m.Durations[jobID][segRes] = duration
	return nil
}

func (m *MockStateStore) GetAllDurations(ctx context.Context, jobID string) (map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return nil, m.Err
	}
	res := make(map[string]string)
	for k, v := range m.Durations[jobID] {
		res[k] = v
	}
	return res, nil
}

func (m *MockStateStore) AddActiveJob(ctx context.Context, partitionID int, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.ActiveJobs[partitionID] = append(m.ActiveJobs[partitionID], jobID)
	return nil
}

func (m *MockStateStore) RemoveActiveJob(ctx context.Context, partitionID int, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	jobs := m.ActiveJobs[partitionID]
	for i, j := range jobs {
		if j == jobID {
			m.ActiveJobs[partitionID] = append(jobs[:i], jobs[i+1:]...)
			break
		}
	}
	return nil
}

func (m *MockStateStore) GetActiveJobs(ctx context.Context, partitionID int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return nil, m.Err
	}
	return append([]string(nil), m.ActiveJobs[partitionID]...), nil
}

func (m *MockStateStore) CacheManifest(ctx context.Context, jobID string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.Manifests[jobID] = append([]byte(nil), data...)
	return nil
}

func (m *MockStateStore) GetCachedManifest(ctx context.Context, jobID string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Manifests[jobID], nil
}

func (m *MockStateStore) PublishProgress(ctx context.Context, jobID string, update models.ProgressUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.ProgressLogs = append(m.ProgressLogs, update)
	return nil
}

func (m *MockStateStore) ReadProgressStream(ctx context.Context, jobIDs []string, lastIDs []string, blockMs int) ([]infra.StreamEntry, error) {
	if blockMs <= 0 {
		blockMs = 20
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Duration(blockMs) * time.Millisecond):
		return nil, nil
	}
}

func (m *MockStateStore) DeduplicateEvent(ctx context.Context, jobID string) (bool, error) {
	return true, nil
}

func (m *MockStateStore) IncrRateLimit(ctx context.Context, key string, windowSec int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return 0, m.Err
	}
	m.RateLimits[key]++
	return m.RateLimits[key], nil
}

func (m *MockStateStore) ExecuteCompletionPipeline(ctx context.Context, p infra.CompletionPipelineParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	return nil
}

func (m *MockStateStore) DeleteKeys(ctx context.Context, keys ...string) error {
	return nil
}

func (m *MockStateStore) ExpireJobKeys(ctx context.Context, jobID string, ttlSec int) error {
	return nil
}

func (m *MockStateStore) Ping(ctx context.Context) error {
	return nil
}

func (m *MockStateStore) ScanJobKeys(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *MockStateStore) RegisterWorker(ctx context.Context, workerID string, info map[string]interface{}, ttlSec int) error {
	return nil
}

func (m *MockStateStore) GetActiveWorkers(ctx context.Context) (map[string]map[string]string, error) {
	return nil, nil
}

func (m *MockStateStore) Close() error {
	return nil
}
