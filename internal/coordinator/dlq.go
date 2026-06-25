package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
)

// runDLQMonitor subscribes to the Dead Letter Queue for transcoding tasks
// and implements exponential backoff retries before failing the job.
func (c *CoordinatorDaemon) runDLQMonitor(ctx context.Context) {
	err := c.bus.SubscribeDLQ(ctx, func(msg infra.TaskMessage) {
		var task models.SegmentTask
		if err := json.Unmarshal(msg.Data(), &task); err != nil {
			log.Printf("DLQ monitor: failed to parse task: %v", err)
			msg.Ack() // Ack bad payloads so they don't block the queue
			return
		}

		// Check if we own the partition for this job
		if c.ring.OwnerOf(task.PartitionID) != c.nodeID {
			// Another coordinator owns this partition, let them handle it.
			// Nak with delay so the owning coordinator can pick it up without spinning.
			msg.NakWithDelay(2 * time.Second)
			return
		}

		log.Printf("DLQ monitor: received failed task for job %s (segment %d, res %s)", 
			task.JobID, task.SegmentIdx, task.Resolution)

		// Check/increment coordinator-level retry counter in Redis (expires in 24h)
		retryKey := fmt.Sprintf("task:{%s}:%d:%s:retries", task.JobID, task.SegmentIdx, task.Resolution)
		retryCount, err := c.state.IncrRateLimit(ctx, retryKey, 86400)
		if err != nil {
			log.Printf("DLQ monitor: failed to increment retry count for job %s: %v", task.JobID, err)
			// Fallback to retryCount = 1 to allow execution
			retryCount = 1
		}

		maxCoordinatorRetries := 3
		if int(retryCount) <= maxCoordinatorRetries {
			// Exponential backoff: retry 1 = 10s, retry 2 = 20s, retry 3 = 40s
			backoff := time.Duration((1 << retryCount) * 5) * time.Second
			log.Printf("DLQ monitor: scheduling retry %d/%d for job %s (segment %d) in %v",
				retryCount, maxCoordinatorRetries, task.JobID, task.SegmentIdx, backoff)

			go func(t models.SegmentTask, delay time.Duration) {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					payload, _ := json.Marshal(t)
					denominator := c.cfg.Coordinator.PartitionCount / c.cfg.Coordinator.NATSShardCount
					var shard int
					if denominator <= 0 {
						shard = t.PartitionID % c.cfg.Coordinator.NATSShardCount
					} else {
						shard = t.PartitionID / denominator
					}
					if err := c.bus.PublishTaskAsync(context.Background(), shard, t.Priority, payload); err != nil {
						log.Printf("DLQ monitor: failed to republish task: %v", err)
						return
					}
					c.bus.FlushPendingPublishes(context.Background())
					log.Printf("DLQ monitor: successfully rescheduled job %s (segment %d)", t.JobID, t.SegmentIdx)
				}
			}(task, backoff)

			msg.Ack()
		} else {
			log.Printf("DLQ monitor: task for job %s (segment %d) exceeded max retries (%d). Failing job.",
				task.JobID, task.SegmentIdx, maxCoordinatorRetries)

			// Mark job as failed
			c.state.SetJobStatus(ctx, task.JobID, map[string]interface{}{
				"state":        string(models.JobPhaseFailed),
				"error":        "transcoding task exceeded max retries in DLQ",
				"last_updated": time.Now().Unix(),
			})

			// Notify progress
			c.state.PublishProgress(ctx, task.JobID, models.ProgressUpdate{
				Phase: models.JobPhaseFailed,
				Error: "transcoding task exceeded max retries",
			})

			// Remove from active jobs
			c.state.RemoveActiveJob(ctx, task.PartitionID, task.JobID)
			msg.Ack()
		}
	})

	if err != nil {
		log.Printf("Failed to start DLQ monitor: %v", err)
	}
}
