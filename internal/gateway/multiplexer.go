package gateway

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
)

// ProgressMultiplexer manages a single Redis XREAD BLOCK loop that fans out
// progress updates to all active WebSocket connections.
type ProgressMultiplexer struct {
	mu          sync.RWMutex
	subscribers map[string][]chan<- models.ProgressUpdate // jobID → list of WebSocket channels
	state       infra.StateStore
	blockMs     int // XREAD BLOCK timeout (e.g. 1000ms)
}

func NewProgressMultiplexer(state infra.StateStore, blockMs int) *ProgressMultiplexer {
	return &ProgressMultiplexer{
		subscribers: make(map[string][]chan<- models.ProgressUpdate),
		state:       state,
		blockMs:     blockMs,
	}
}

// Subscribe adds a WebSocket channel to receive updates for a specific job.
func (pm *ProgressMultiplexer) Subscribe(jobID string, ch chan<- models.ProgressUpdate) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.subscribers[jobID] = append(pm.subscribers[jobID], ch)
}

// Unsubscribe removes a WebSocket channel.
func (pm *ProgressMultiplexer) Unsubscribe(jobID string, ch chan<- models.ProgressUpdate) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	subs := pm.subscribers[jobID]
	for i, s := range subs {
		if s == ch {
			pm.subscribers[jobID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(pm.subscribers[jobID]) == 0 {
		delete(pm.subscribers, jobID)
	}
}

// Run is the single background goroutine that fans out Redis Stream
// updates to all subscribed WebSockets. It reduces Redis connections
// from 50,000 (one per WebSocket) to 1 per Gateway node.
func (pm *ProgressMultiplexer) Run(ctx context.Context) {
	// Track the last-seen Stream ID per job for XREAD resume
	lastIDs := make(map[string]string) // jobID → last stream entry ID

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pm.mu.RLock()
		if len(pm.subscribers) == 0 {
			pm.mu.RUnlock()
			// B-8 fix: sleep to avoid busy-loop when no WebSockets are active
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Build the list of stream keys and last IDs for XREAD
		jobIDs := make([]string, 0, len(pm.subscribers))
		streamLastIDs := make([]string, 0, len(pm.subscribers))
		for jobID := range pm.subscribers {
			jobIDs = append(jobIDs, jobID)
			id, ok := lastIDs[jobID]
			if !ok {
				id = "0" // read from beginning on first subscribe
			}
			streamLastIDs = append(streamLastIDs, id)
		}
		pm.mu.RUnlock()

		// Prune lastIDs map to prevent memory leak
		pm.mu.RLock()
		for jobID := range lastIDs {
			if _, active := pm.subscribers[jobID]; !active {
				delete(lastIDs, jobID)
			}
		}
		pm.mu.RUnlock()

		// Single multiplexed XREAD BLOCK call for ALL active jobs
		entries, err := pm.state.ReadProgressStream(ctx, jobIDs, streamLastIDs, pm.blockMs)
		if err != nil {
			continue // XREAD timeout or transient error
		}

		// Fan out each entry to its subscribers
		pm.mu.RLock()
		for _, entry := range entries {
			lastIDs[entry.JobID] = entry.ID
			subs, ok := pm.subscribers[entry.JobID]
			if !ok {
				continue
			}
			update := parseProgressUpdate(entry.Fields)
			for _, ch := range subs {
				select {
				case ch <- update:
				default:
					// Drop if client is slow — they will get a snapshot on reconnect
				}
			}
		}
		pm.mu.RUnlock()
	}
}

func parseProgressUpdate(fields map[string]string) models.ProgressUpdate {
	var update models.ProgressUpdate
	update.Phase = models.JobPhase(fields["phase"])
	
	if val, ok := fields["completed"]; ok {
		i, _ := strconv.Atoi(val)
		update.Completed = i
	}
	if val, ok := fields["total"]; ok {
		i, _ := strconv.Atoi(val)
		update.Total = i
	}
	if val, ok := fields["pct"]; ok {
		i, _ := strconv.Atoi(val)
		update.Percent = i
	}
	
	update.HLSURL = fields["hls_url"]
	update.DASHURL = fields["dash_url"]
	update.Error = fields["error"]
	
	return update
}
