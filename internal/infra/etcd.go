package infra

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/distributed-transcoder/internal/config"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// Coordination abstracts etcd operations for coordinator registration and locking.
type Coordination interface {
	// Registration
	Register(ctx context.Context, nodeID string, leaseTTLSec int) (leaseID int64, err error)
	Deregister(ctx context.Context, nodeID string) error
	WatchCoordinators(ctx context.Context) (<-chan CoordinatorEvent, error)
	GetCoordinators(ctx context.Context) ([]string, error)

	// Slicing Locks
	AcquireSlicingLock(ctx context.Context, jobID string, ownerID string, ttlSec int) (bool, error)
	ReleaseSlicingLock(ctx context.Context, jobID string) error
	KeepAliveLock(ctx context.Context, leaseID int64) error

	// Health
	Ping(ctx context.Context) error

	Close() error
}

type CoordinatorEvent struct {
	Type   EventType // PUT or DELETE
	NodeID string
	Host   string
}

type EventType int

const (
	EventTypePut EventType = iota
	EventTypeDelete
)

type EtcdClient struct {
	client    *clientv3.Client
	mu        sync.Mutex
	sessions  map[string]*concurrency.Session
	mutexes   map[string]*concurrency.Mutex
}

func NewEtcdClient(cfg config.EtcdConfig) (*EtcdClient, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	return &EtcdClient{
		client:   client,
		sessions: make(map[string]*concurrency.Session),
		mutexes:  make(map[string]*concurrency.Mutex),
	}, nil
}

func (e *EtcdClient) Register(ctx context.Context, nodeID string, leaseTTLSec int) (int64, error) {
	lease, err := e.client.Grant(ctx, int64(leaseTTLSec))
	if err != nil {
		return 0, err
	}

	key := fmt.Sprintf("/coordinators/%s", nodeID)
	// Put with lease
	_, err = e.client.Put(ctx, key, "active", clientv3.WithLease(lease.ID))
	if err != nil {
		return 0, err
	}

	// Keep alive in the background
	ch, err := e.client.KeepAlive(ctx, lease.ID)
	if err != nil {
		return 0, err
	}
	go func() {
		for range ch {
			// drain to prevent queue-full deadlocks
		}
	}()

	return int64(lease.ID), nil
}

func (e *EtcdClient) Deregister(ctx context.Context, nodeID string) error {
	key := fmt.Sprintf("/coordinators/%s", nodeID)
	_, err := e.client.Delete(ctx, key)
	return err
}

func (e *EtcdClient) WatchCoordinators(ctx context.Context) (<-chan CoordinatorEvent, error) {
	ch := make(chan CoordinatorEvent, 100)

	log.Printf("[DEBUG-ETCD] WatchCoordinators: doing initial fetch...")
	resp, err := e.client.Get(ctx, "/coordinators/", clientv3.WithPrefix())
	if err != nil {
		log.Printf("[DEBUG-ETCD] WatchCoordinators initial fetch error: %v", err)
		return nil, err
	}
	log.Printf("[DEBUG-ETCD] WatchCoordinators: initial fetch found %d keys", len(resp.Kvs))

	go func() {
		defer close(ch) // H-2 fix: ensure consumer goroutine isn't stuck on <-events forever

		for _, kv := range resp.Kvs {
			key := string(kv.Key)
			parts := strings.Split(key, "/")
			if len(parts) > 2 {
				log.Printf("[DEBUG-ETCD] WatchCoordinators: sending initial key %s", parts[2])
				// H-1 fix: ctx-aware send to prevent goroutine leak if channel fills up
				select {
				case ch <- CoordinatorEvent{
					Type:   EventTypePut,
					NodeID: parts[2],
					Host:   string(kv.Value),
				}:
				case <-ctx.Done():
					return
				}
			}
		}

		log.Printf("[DEBUG-ETCD] WatchCoordinators: starting etcd watch...")
		watchChan := e.client.Watch(ctx, "/coordinators/", clientv3.WithPrefix())
		for watchResp := range watchChan {
			for _, event := range watchResp.Events {
				key := string(event.Kv.Key)
				parts := strings.Split(key, "/")
				if len(parts) > 2 {
					nodeID := parts[2]
					evType := EventTypePut
					if event.Type == clientv3.EventTypeDelete {
						evType = EventTypeDelete
					}
					log.Printf("[DEBUG-ETCD] WatchCoordinators: watch event type %d on node %s", evType, nodeID)
					select {
					case ch <- CoordinatorEvent{
						Type:   evType,
						NodeID: nodeID,
						Host:   string(event.Kv.Value),
					}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch, nil
}

func (e *EtcdClient) AcquireSlicingLock(ctx context.Context, jobID string, ownerID string, ttlSec int) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// If session already exists, we already hold the lock
	if _, exists := e.sessions[jobID]; exists {
		return true, nil
	}

	session, err := concurrency.NewSession(e.client, concurrency.WithTTL(ttlSec))
	if err != nil {
		return false, err
	}

	mutex := concurrency.NewMutex(session, fmt.Sprintf("/locks/slicing/%s", jobID))
	err = mutex.TryLock(ctx)
	if err != nil {
		session.Close() // terminate lease keepalive goroutine immediately
		if err == concurrency.ErrLocked {
			return false, nil
		}
		return false, err
	}

	e.sessions[jobID] = session
	e.mutexes[jobID] = mutex
	return true, nil
}

func (e *EtcdClient) ReleaseSlicingLock(ctx context.Context, jobID string) error {
	e.mu.Lock()
	session, hasSession := e.sessions[jobID]
	mutex, hasMutex := e.mutexes[jobID]
	if hasSession {
		delete(e.sessions, jobID)
	}
	if hasMutex {
		delete(e.mutexes, jobID)
	}
	e.mu.Unlock()

	if hasMutex && mutex != nil {
		mutex.Unlock(ctx)
	}
	if hasSession && session != nil {
		session.Close()
	}

	// Simple delete since it's an ephemeral lock. A proper implementation would use the Mutex.Unlock
	_, err := e.client.Delete(ctx, fmt.Sprintf("/locks/slicing/%s", jobID), clientv3.WithPrefix())
	return err
}

func (e *EtcdClient) KeepAliveLock(ctx context.Context, leaseID int64) error {
	_, err := e.client.KeepAliveOnce(ctx, clientv3.LeaseID(leaseID))
	return err
}

func (e *EtcdClient) Ping(ctx context.Context) error {
	if e == nil || e.client == nil {
		return fmt.Errorf("etcd client not initialized")
	}
	// Status on first endpoint
	if len(e.client.Endpoints()) > 0 {
		_, err := e.client.Status(ctx, e.client.Endpoints()[0])
		return err
	}
	return fmt.Errorf("no etcd endpoints configured")
}

func (e *EtcdClient) GetCoordinators(ctx context.Context) ([]string, error) {
	if e == nil || e.client == nil {
		return nil, fmt.Errorf("etcd client not initialized")
	}
	resp, err := e.client.Get(ctx, "/coordinators/", clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	var nodeIDs []string
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		parts := strings.Split(key, "/")
		if len(parts) > 2 {
			nodeIDs = append(nodeIDs, parts[2])
		}
	}
	return nodeIDs, nil
}

// Close releases all etcd sessions and closes the underlying client connection.
func (e *EtcdClient) Close() error {
	e.mu.Lock()
	for jobID, session := range e.sessions {
		session.Close()
		delete(e.sessions, jobID)
	}
	for jobID := range e.mutexes {
		delete(e.mutexes, jobID)
	}
	e.mu.Unlock()
	return e.client.Close()
}
