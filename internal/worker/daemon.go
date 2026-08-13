package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
)

type WorkerDaemon struct {
	cfg        config.Config
	state      infra.StateStore
	objStore   infra.ObjectStore
	bus        infra.MessageBus
	executor   *TaskExecutor
	wg         sync.WaitGroup
	pullersCtx context.Context
	cancelPull context.CancelFunc
	activeTasks int32
}

func NewWorkerDaemon(cfg config.Config, state infra.StateStore, objStore infra.ObjectStore, bus infra.MessageBus) *WorkerDaemon {
	breaker := NewCircuitBreaker(cfg.Worker.CircuitBreakerWindow, cfg.Worker.CircuitBreakerThresh)
	executor := NewTaskExecutor(state, objStore, cfg, breaker)

	pCtx, pCancel := context.WithCancel(context.Background())

	return &WorkerDaemon{
		cfg:        cfg,
		state:      state,
		objStore:   objStore,
		bus:        bus,
		executor:   executor,
		pullersCtx: pCtx,
		cancelPull: pCancel,
	}
}

func (w *WorkerDaemon) Run(ctx context.Context) error {
	taskCh := make(chan infra.TaskMessage, w.cfg.Worker.ConcurrentTasks*2)
	var pullerWg sync.WaitGroup

	// Start task pullers (1 per shard assigned to this worker)
	// For simplicity in this LLD implementation, we'll pull from shards 0 to PartitionCount
	// In reality, this would be dynamically assigned or worker listens to all shards.
	for i := 0; i < w.cfg.Coordinator.NATSShardCount; i++ {
		pullerWg.Add(1)
		go w.taskPuller(w.pullersCtx, i, taskCh, &pullerWg)
	}

	// Start executor pool
	for i := 0; i < w.cfg.Worker.ConcurrentTasks; i++ {
		w.wg.Add(1)
		go w.executorWorker(taskCh)
	}

	log.Printf("Worker %s started with %d concurrent tasks", w.cfg.NodeID, w.cfg.Worker.ConcurrentTasks)

	// Heartbeat worker status to Redis
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tasks := atomic.LoadInt32(&w.activeTasks)
				cpu := int32(5) + tasks*15
				if cpu > 95 {
					cpu = 95
				}
				gpu := tasks * 20
				if gpu > 100 {
					gpu = 100
				}
				// Add small jitter
				cpu += int32(time.Now().UnixNano() % 5)
				gpu += int32(time.Now().UnixNano() % 5)

				info := map[string]interface{}{
					"id":    w.cfg.NodeID,
					"cpu":   cpu,
					"gpu":   gpu,
					"tasks": tasks,
				}
				// L-3 fix: use short-timeout context instead of context.Background()
				// to prevent hangs during shutdown
				hbCtx, hbCancel := context.WithTimeout(ctx, 2*time.Second)
				w.state.RegisterWorker(hbCtx, w.cfg.NodeID, info, 6) // 6s TTL
				hbCancel()
			}
		}
	}()

	<-ctx.Done() // SIGTERM received

	// 1. Stop pulling new tasks from NATS
	w.stopPullers()

	// Wait for pullers to finish, then close the task channel
	pullerWg.Wait()
	close(taskCh)

	// 2. Wait for in-flight tasks to complete (up to GracefulDrainSec minutes)
	drainCtx, drainCancel := context.WithTimeout(context.Background(),
		time.Duration(w.cfg.Worker.GracefulDrainSec)*time.Second)
	defer drainCancel()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All tasks completed gracefully")
	case <-drainCtx.Done():
		log.Println("Drain timeout reached, killing remaining FFmpeg processes")
		w.killAllFFmpeg()
		// Unacked tasks will be redelivered by NATS AckWait
	}

	return nil
}

func (w *WorkerDaemon) taskPuller(ctx context.Context, shard int, taskCh chan<- infra.TaskMessage, wg *sync.WaitGroup) {
	defer wg.Done()
	
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Apply backpressure: do not pull if task channel has buffered tasks waiting
		if len(taskCh) >= w.cfg.Worker.ConcurrentTasks {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}

		msgs, err := w.bus.PullTasks(ctx, shard, 10)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
			continue
		}


		for i, msg := range msgs {
			select {
			case taskCh <- msg:
			case <-ctx.Done():
				// NAK all remaining messages
				for j := i; j < len(msgs); j++ {
					msgs[j].Nak()
				}
				return
			}
		}
	}
}

func (w *WorkerDaemon) executorWorker(taskCh <-chan infra.TaskMessage) {
	defer w.wg.Done()

	for msg := range taskCh {
		if msg == nil {
			continue
		}

		var segmentTask models.SegmentTask
		if err := json.Unmarshal(msg.Data(), &segmentTask); err != nil {
			log.Printf("failed to unmarshal task: %v", err)
			msg.Ack() // Invalid payload, discard
			continue
		}

		atomic.AddInt32(&w.activeTasks, 1)
		err := w.executor.Execute(context.Background(), msg, segmentTask)
		atomic.AddInt32(&w.activeTasks, -1)
		if err != nil {
			log.Printf("Task execution failed: %v", err)
			msg.Nak()
		} else {
			// Resilient fallback completion event to coordinator
			completionSubject := fmt.Sprintf("s3-transcoded.job.partition_%d.job_%s", segmentTask.PartitionID, segmentTask.JobID)
			eventBytes, _ := json.Marshal(segmentTask)
			if err := w.bus.PublishEvent(context.Background(), completionSubject, eventBytes); err != nil {
				log.Printf("Failed to publish completion event: %v", err)
			}
		}
	}
}

func (w *WorkerDaemon) stopPullers() {
	w.cancelPull()
}

func (w *WorkerDaemon) killAllFFmpeg() {
	// A brute-force fallback to kill all ffmpeg processes on the system
	// In production, we'd track PIDs or use cgroup killing.
	exec.Command("pkill", "-9", "ffmpeg").Run()
}
