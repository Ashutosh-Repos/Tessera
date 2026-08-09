package mocks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/distributed-transcoder/internal/infra"
)

// MockObjectStore implements infra.ObjectStore for testing.
type MockObjectStore struct {
	mu           sync.RWMutex
	Objects      map[string][]byte
	PutCount     int
	CopyCount    int
	DeleteCount  int
	ListPrefixes map[string][]string
	Err          error
}

func NewMockObjectStore() *MockObjectStore {
	return &MockObjectStore{
		Objects:      make(map[string][]byte),
		ListPrefixes: make(map[string][]string),
	}
}

func (m *MockObjectStore) CreateMultipartUpload(ctx context.Context, key string) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	return "mock-upload-id-12345", nil
}

func (m *MockObjectStore) GeneratePresignedPUT(ctx context.Context, key, uploadID string, partNum int, expiry time.Duration) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	return fmt.Sprintf("http://mock-s3/upload/%s?part=%d", key, partNum), nil
}

func (m *MockObjectStore) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []infra.CompletedPart) error {
	if m.Err != nil {
		return m.Err
	}
	return nil
}

func (m *MockObjectStore) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	if m.Err != nil {
		return m.Err
	}
	return nil
}

func (m *MockObjectStore) PutObject(ctx context.Context, key string, body io.Reader, size int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.PutCount++
	buf := new(bytes.Buffer)
	if body != nil {
		_, _ = buf.ReadFrom(body)
	}
	m.Objects[key] = buf.Bytes()
	return nil
}

func (m *MockObjectStore) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return nil, m.Err
	}
	data, ok := m.Objects[key]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *MockObjectStore) HeadObject(ctx context.Context, key string) (infra.ObjectMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return infra.ObjectMeta{Exists: false}, m.Err
	}
	data, ok := m.Objects[key]
	if !ok {
		return infra.ObjectMeta{Exists: false}, nil
	}
	return infra.ObjectMeta{
		Key:          key,
		Exists:       true,
		Size:         int64(len(data)),
		LastModified: time.Now(),
	}, nil
}

func (m *MockObjectStore) CopyObject(ctx context.Context, srcKey, dstKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.CopyCount++
	data, ok := m.Objects[srcKey]
	if !ok {
		return fmt.Errorf("src object not found: %s", srcKey)
	}
	m.Objects[dstKey] = append([]byte(nil), data...)
	return nil
}

func (m *MockObjectStore) DeleteObject(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.DeleteCount++
	delete(m.Objects, key)
	return nil
}

func (m *MockObjectStore) DeletePrefix(ctx context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	for k := range m.Objects {
		if strings.HasPrefix(k, prefix) {
			delete(m.Objects, k)
		}
	}
	return nil
}

func (m *MockObjectStore) ListObjectsPrefix(ctx context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Err != nil {
		return nil, m.Err
	}
	if list, ok := m.ListPrefixes[prefix]; ok {
		return list, nil
	}
	var res []string
	for k := range m.Objects {
		if strings.HasPrefix(k, prefix) {
			res = append(res, k)
		}
	}
	return res, nil
}

func (m *MockObjectStore) Ping(ctx context.Context) error {
	return m.Err
}
