package coordinator

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/infra"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type CoordinatorDaemon struct {
	cfg          config.Config
	nodeID       string
	ring         *HashRing
	state        infra.StateStore
	bus          infra.MessageBus
	coord        infra.Coordination
	objStore     infra.ObjectStore
	sliceSem     chan struct{} // buffered channel of size cfg.SlicingSemaphore
	currentEpoch int64         // monotonic epoch counter, incremented on each registration
	ctx          context.Context // daemon lifecycle context

	mu         sync.Mutex
	partitions map[int]*PartitionManager // active partition managers
	fenced     bool                      // true if self-fenced
}

func NewCoordinatorDaemon(cfg config.Config, nodeID string, state infra.StateStore, bus infra.MessageBus, coord infra.Coordination, objStore infra.ObjectStore) *CoordinatorDaemon {
	return &CoordinatorDaemon{
		cfg:        cfg,
		nodeID:     nodeID,
		state:      state,
		bus:        bus,
		coord:      coord,
		objStore:   objStore,
		sliceSem:   make(chan struct{}, cfg.Coordinator.SlicingSemaphore),
		partitions: make(map[int]*PartitionManager),
		ring:       NewHashRing(),
	}
}

// Run is the coordinator's main entry point.
func (c *CoordinatorDaemon) Run(ctx context.Context) {
	c.ctx = ctx // store for partition managers

	// D-7 fix: ensure partition count is evenly divisible by shard count
	if c.cfg.Coordinator.PartitionCount%c.cfg.Coordinator.NATSShardCount != 0 {
		log.Fatalf("partition_count (%d) must be divisible by nats_shard_count (%d)",
			c.cfg.Coordinator.PartitionCount, c.cfg.Coordinator.NATSShardCount)
	}

	// 1. Ring Watcher
	events, err := c.coord.WatchCoordinators(ctx)
	if err != nil {
		log.Fatalf("failed to watch coordinators: %v", err)
	}
	go func() {
		activeNodes := make(map[string]string)
		log.Printf("[DEBUG-COORD] Ring Watcher goroutine started")
		for {
			select {
			case <-ctx.Done():
				log.Printf("[DEBUG-COORD] Ring Watcher goroutine exiting (context cancelled)")
				return
			case evt, ok := <-events:
				if !ok {
					log.Printf("[DEBUG-COORD] Ring Watcher channel closed, exiting")
					return
				}
				log.Printf("[DEBUG-COORD] Ring Watcher received event: type=%d, node=%s", evt.Type, evt.NodeID)
				switch evt.Type {
				case infra.EventTypePut:
					activeNodes[evt.NodeID] = evt.Host
				case infra.EventTypeDelete:
					delete(activeNodes, evt.NodeID)
				}
				nodeIDs := make([]string, 0, len(activeNodes))
				for nid := range activeNodes {
					nodeIDs = append(nodeIDs, nid)
				}
				log.Printf("[DEBUG-COORD] Ring Watcher invoking onRingChange with nodes: %v", nodeIDs)
				c.onRingChange(nodeIDs)
			}
		}
	}()

	// 2. etcd Registration + Self-Fencing
	go c.runEtcdRegistration(ctx)

	// 3. DLQ Monitor
	go c.runDLQMonitor(ctx)

	// 4. Job GC Daemon
	gc := &JobGCDaemon{
		coord:          c,
		intervalMin:    c.cfg.Coordinator.GCIntervalMin,
		staleThreshSec: int64(c.cfg.Coordinator.GCStaleThreshHours * 3600),
	}
	go gc.Run(ctx)

	// 5. Metrics Server
	go func() {
		mux := http.NewServeMux()
		mux.Handle(c.cfg.Metrics.Path, promhttp.Handler())
		srv := &http.Server{Addr: c.cfg.Metrics.ListenAddr, Handler: mux}
		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()
		srv.ListenAndServe()
	}()

	// 6. Block until shutdown
	<-ctx.Done()

	// 7. Graceful drain
	log.Println("coordinator shutting down: releasing partitions")
	c.selfFence()
	c.coord.Deregister(context.Background(), c.nodeID)
}

func (c *CoordinatorDaemon) runEtcdRegistration(ctx context.Context) {
	// Register with etcd and maintain lease. If lease expires, selfFence().
	// Simplified implementation for LLD context:
	leaseID, err := c.coord.Register(ctx, c.nodeID, c.cfg.Coordinator.EtcdLeaseTTLSec)
	if err != nil {
		log.Fatalf("Failed to register with etcd: %v", err)
	}

	ticker := time.NewTicker(time.Duration(c.cfg.Coordinator.EtcdLeaseTTLSec/2) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.coord.KeepAliveLock(ctx, leaseID); err != nil {
				log.Printf("Lost etcd lease: %v, self-fencing", err)
				c.selfFence()
				// Try to re-register
				newLease, reErr := c.coord.Register(ctx, c.nodeID, c.cfg.Coordinator.EtcdLeaseTTLSec)
				if reErr == nil {
					log.Printf("Re-registered with etcd")
					leaseID = newLease
					c.mu.Lock()
					c.fenced = false
					c.mu.Unlock()
					c.onRingChange(c.ring.members) // Re-evaluate partitions
				}
			}
		}
	}
}

func (c *CoordinatorDaemon) selfFence() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fenced = true
	c.currentEpoch++

	// Cancel all active partition managers
	for id, pm := range c.partitions {
		log.Printf("Fencing partition %d", id)
		pm.Stop()
		delete(c.partitions, id)
	}
}

func (c *CoordinatorDaemon) onRingChange(activeNodes []string) {
	c.mu.Lock()
	if c.fenced {
		c.mu.Unlock()
		return
	}
	c.ring.Rebuild(activeNodes)
	owned := c.ring.OwnedPartitions(c.nodeID, c.cfg.Coordinator.PartitionCount)
	
	// Map to O(1) lookup
	ownedMap := make(map[int]bool)
	for _, p := range owned {
		ownedMap[p] = true
	}

	// 1. Release partitions we no longer own
	for id, pm := range c.partitions {
		if !ownedMap[id] {
			log.Printf("Relinquishing partition %d", id)
			pm.Stop()
			delete(c.partitions, id)
		}
	}

	// 2. Adopt new partitions we now own
	// Collect new partitions to launch outside the lock
	type newPM struct {
		id int
		pm *PartitionManager
	}
	var toStart []newPM
	for _, p := range owned {
		if _, exists := c.partitions[p]; !exists {
			log.Printf("Adopting partition %d", p)
			pm := NewPartitionManager(c, p, c.currentEpoch)
			c.partitions[p] = pm
			toStart = append(toStart, newPM{id: p, pm: pm})
		}
	}
	c.mu.Unlock()

	// H-3/M-1 fix: launch goroutines outside the lock with the daemon context
	for _, ts := range toStart {
		go ts.pm.Start(c.ctx)
	}
}
