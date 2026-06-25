package coordinator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/distributed-transcoder/internal/models"
)

type JobGCDaemon struct {
	coord          *CoordinatorDaemon
	intervalMin    int
	staleThreshSec int64
}

func (gc *JobGCDaemon) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(gc.intervalMin) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gc.scanForStaleJobs(ctx)
		}
	}
}

func (gc *JobGCDaemon) scanForStaleJobs(ctx context.Context) {
	// Only scan partitions owned by this coordinator
	owned := gc.coord.ring.OwnedPartitions(gc.coord.nodeID, gc.coord.cfg.Coordinator.PartitionCount)
	now := time.Now().Unix()

	for _, pid := range owned {
		jobIDs, err := gc.coord.state.GetActiveJobs(ctx, pid)
		if err != nil {
			continue
		}

		for _, jobID := range jobIDs {
			status, err := gc.coord.state.GetJobStatus(ctx, jobID)
			if err != nil {
				continue
			}

			lastUpdated := parseInt64(status["last_updated"])
			if now-lastUpdated > gc.staleThreshSec {
				log.Printf("Job %s in partition %d is stale (inactive for %ds), marking failed", jobID, pid, now-lastUpdated)

				// Mark as failed
				gc.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
					"state":        string(models.JobPhaseFailed),
					"error":        "job timed out",
					"last_updated": now,
				})
				gc.coord.state.PublishProgress(ctx, jobID, models.ProgressUpdate{
					Phase: models.JobPhaseFailed,
					Error: "job timed out due to inactivity",
				})

				// Clean up raw files and slices from S3 to prevent disk leaks
				rawPrefix := fmt.Sprintf("jobs/partition_%d/job_%s/raw/", pid, jobID)
				if err := gc.coord.objStore.DeletePrefix(ctx, rawPrefix); err != nil {
					log.Printf("GC Job %s: failed to clean up raw S3 files: %v", jobID, err)
				}

				// Expire Redis keys after 24h to prevent memory leaks
				if err := gc.coord.state.ExpireJobKeys(ctx, jobID, 86400); err != nil {
					log.Printf("GC Job %s: failed to set Redis keys expiration: %v", jobID, err)
				}

				// Remove from active jobs
				gc.coord.state.RemoveActiveJob(ctx, pid, jobID)
			}
		}
	}
}
