package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/models"
)

// PartitionManager handles the full lifecycle of jobs assigned to a single partition.
type PartitionManager struct {
	partitionID int
	coord       *CoordinatorDaemon
	ctx         context.Context
	cancelFn    context.CancelFunc
}

func NewPartitionManager(coord *CoordinatorDaemon, partitionID int, epoch int64) *PartitionManager {
	return &PartitionManager{
		partitionID: partitionID,
		coord:       coord,
	}
}

func (pm *PartitionManager) Start(ctx context.Context) {
	pmCtx, cancel := context.WithCancel(ctx)
	pm.ctx = pmCtx
	pm.cancelFn = cancel

	// 1. Reconstruct state from Redis (Tier 1) or S3 (Tier 3)
	pm.reconstructState(pmCtx)

	// 2. Subscribe to upload events for this partition
	if err := pm.coord.bus.SubscribePartitionUploads(pmCtx, pm.partitionID, pm.handleUploadEvent); err != nil {
		log.Printf("Failed to subscribe to uploads for partition %d: %v", pm.partitionID, err)
	}

	// 3. Subscribe to task completion events for this partition
	if err := pm.coord.bus.SubscribeCompletionEvents(pmCtx, pm.partitionID, pm.handleCompletionEvent); err != nil {
		log.Printf("Failed to subscribe to completions for partition %d: %v", pm.partitionID, err)
	}
}

func (pm *PartitionManager) Stop() {
	if pm.cancelFn != nil {
		pm.cancelFn()
	}
}

// reconstructState rebuilds the partition's active job state using the 3-tier
// fallback strategy defined in HLD §4.3. (I-13 fix)
//
//	Tier 1: Redis fast path (<50ms) — read active_jobs set + per-job status
//	Tier 3: S3 full scan (5-30s) — list all job dirs, skip completed, rebuild Redis
func (pm *PartitionManager) reconstructState(ctx context.Context) {
	// ──── Tier 1: Redis Fast Path ────
	jobIDs, err := pm.coord.state.GetActiveJobs(ctx, pm.partitionID)
	if err != nil || len(jobIDs) == 0 {
		// Redis unavailable or empty → fall through to S3 scan
		pm.reconstructFromS3(ctx)
		return
	}

	for _, jobID := range jobIDs {
		status, err := pm.coord.state.GetJobStatus(ctx, jobID)
		if err != nil {
			continue
		}

		phase := status["state"]
		// Skip terminal states
		if phase == string(models.JobPhaseCompleted) || phase == string(models.JobPhaseFailed) {
			pm.coord.state.RemoveActiveJob(ctx, pm.partitionID, jobID)
			continue
		}

		// ──── Tier 2: Backfill manifest cache if missing ────
		_, cacheErr := pm.coord.state.GetCachedManifest(ctx, jobID)
		if cacheErr != nil {
			manifest, mErr := pm.loadManifest(ctx, jobID)
			if mErr == nil {
				data, _ := json.Marshal(manifest)
				pm.coord.state.CacheManifest(ctx, jobID, data)
			}
		}

		// ──── Check if job completed while partition was orphaned ────
		total := parseInt(status["total"])
		if total > 0 {
			count, _ := pm.coord.state.BitCount(ctx, jobID)
			if int(count) >= total {
				// All tasks done — trigger manifest compilation if not already done
				first, _ := pm.coord.state.DeduplicateEvent(ctx, jobID+":manifest")
				if first {
					go pm.compileManifests(ctx, jobID, total)
				}
			}
		}
	}
	log.Printf("partition %d: reconstructed %d active jobs from Redis", pm.partitionID, len(jobIDs))
}

// reconstructFromS3 is the Tier 3 fallback that scans S3 for active jobs.
func (pm *PartitionManager) reconstructFromS3(ctx context.Context) {
	prefix := fmt.Sprintf("jobs/partition_%d/", pm.partitionID)
	keys, err := pm.coord.objStore.ListObjectsPrefix(ctx, prefix)
	if err != nil {
		log.Printf("partition %d: S3 reconstruction failed: %v", pm.partitionID, err)
		return
	}

	seen := make(map[string]bool)
	for _, key := range keys {
		jobID := extractJobID(key)
		if seen[jobID] {
			continue
		}
		seen[jobID] = true

		// Enforce regional isolation: only reconstruct jobs belonging to our region
		parts := strings.Split(jobID, ":")
		if len(parts) > 1 && parts[0] != pm.coord.cfg.Region {
			continue // skip jobs from other regions
		}

		// Check for completion sentinel
		sentinelKey := fmt.Sprintf("jobs/partition_%d/job_%s/job_completed.json", pm.partitionID, jobID)
		meta, _ := pm.coord.objStore.HeadObject(ctx, sentinelKey)
		if meta.Exists {
			continue // job already completed — skip
		}

		// Active job found — rebuild Redis state
		pm.coord.state.AddActiveJob(ctx, pm.partitionID, jobID)

		manifest, mErr := pm.loadManifest(ctx, jobID)
		if mErr != nil {
			continue
		}
		data, _ := json.Marshal(manifest)
		pm.coord.state.CacheManifest(ctx, jobID, data)

		// Count transcoded segments to rebuild bitmap
		transPrefix := fmt.Sprintf("jobs/partition_%d/job_%s/transcoded/", pm.partitionID, jobID)
		transKeys, _ := pm.coord.objStore.ListObjectsPrefix(ctx, transPrefix)
		completed := len(transKeys)

		totalTasks := manifest.TotalTasks
		if totalTasks == 0 && manifest.SegmentCount > 0 {
			totalTasks = manifest.SegmentCount * len(manifest.Resolutions)
		}
		if totalTasks == 0 {
			totalTasks = len(manifest.Resolutions) * 1 // fallback
		}

		pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
			"state":        string(models.JobPhaseTranscoding),
			"completed":    completed,
			"total":        totalTasks,
			"partition":    pm.partitionID,
			"last_updated": time.Now().Unix(),
		})

		// Rebuild bitmap from S3 object existence
		for _, tk := range transKeys {
			seg, res := parseSegmentKey(tk)
			bitIdx := seg*len(models.AllResolutions) + resolutionOffset(res)
			pm.coord.state.SetBit(ctx, jobID, bitIdx)
		}

		// Check if reconstruction reveals a completed job
		if completed >= totalTasks && totalTasks > 0 {
			first, _ := pm.coord.state.DeduplicateEvent(ctx, jobID+":manifest")
			if first {
				go pm.compileManifests(ctx, jobID, totalTasks)
			}
		}
	}
	log.Printf("partition %d: reconstructed %d active jobs from S3", pm.partitionID, len(seen))
}

func (pm *PartitionManager) handleUploadEvent(msg infra.TaskMessage) {
	jobID := extractJobID(string(msg.Data()))

	// Enforce regional isolation: only process jobs belonging to our region
	parts := strings.Split(jobID, ":")
	if len(parts) > 1 && parts[0] != pm.coord.cfg.Region {
		log.Printf("partition %d: ignoring upload event for job %s from foreign region", pm.partitionID, jobID)
		msg.Ack()
		return
	}

	// Deduplicate SQS at-least-once delivery
	isFirst, _ := pm.coord.state.DeduplicateEvent(pm.ctx, jobID)
	if !isFirst {
		msg.Ack()
		return
	}

	// B-4 fix: ACK immediately before entering the semaphore queue.
	msg.Ack()

	// Acquire slicing semaphore (blocks if slots full or context cancelled)
	select {
	case pm.coord.sliceSem <- struct{}{}:
	case <-pm.ctx.Done():
		return
	}
	go func() {
		defer func() { <-pm.coord.sliceSem }()
		pm.sliceAndDispatch(pm.ctx, jobID)
	}()
}

func (pm *PartitionManager) handleCompletionEvent(msg infra.TaskMessage) {
	var task models.SegmentTask
	json.Unmarshal(msg.Data(), &task)
	msg.Ack()

	// Enforce regional isolation: only process completion events for jobs belonging to our region
	parts := strings.Split(task.JobID, ":")
	if len(parts) > 1 && parts[0] != pm.coord.cfg.Region {
		log.Printf("partition %d: ignoring completion event for job %s from foreign region", pm.partitionID, task.JobID)
		return
	}

	// Check BITCOUNT → if all tasks done, compile manifests
	count, _ := pm.coord.state.BitCount(pm.ctx, task.JobID)
	status, _ := pm.coord.state.GetJobStatus(pm.ctx, task.JobID)
	total := parseInt(status["total"])

	if int(count) >= total && total > 0 {
		// D-6 fix: Prevent concurrent manifest compilations
		first, _ := pm.coord.state.DeduplicateEvent(pm.ctx, task.JobID+":manifest")
		if first {
			pm.compileManifests(pm.ctx, task.JobID, total)
		}
	}
}

// Utility functions

func extractJobID(data string) string {
	// 1. Try to parse as JSON S3 Event
	var s3Event struct {
		Records []struct {
			S3 struct {
				Object struct {
					Key string `json:"key"`
				} `json:"object"`
			} `json:"s3"`
		} `json:"Records"`
	}
	if err := json.Unmarshal([]byte(data), &s3Event); err == nil && len(s3Event.Records) > 0 {
		key := s3Event.Records[0].S3.Object.Key
		parts := strings.Split(key, "/")
		for _, p := range parts {
			if strings.HasPrefix(p, "job_") {
				return strings.TrimPrefix(p, "job_")
			}
		}
	}
	// 2. Fallback: check if raw data string contains a "job_" path segment
	parts := strings.Split(data, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "job_") {
			return strings.TrimPrefix(p, "job_")
		}
	}
	// 3. Last fallback: treat raw payload as jobID directly
	return data
}

func parseSegmentKey(key string) (int, models.Resolution) {
	base := filepath.Base(key)
	if !strings.HasPrefix(base, "segment_") || !strings.HasSuffix(base, ".ts") {
		return 0, models.Res1080p
	}
	parts := strings.Split(base, "_")
	if len(parts) >= 3 {
		segIdx, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, models.Res1080p
		}
		resStr := strings.TrimSuffix(parts[2], ".ts")
		return segIdx, models.Resolution(resStr)
	}
	return 0, models.Res1080p
}

func resolutionOffset(res models.Resolution) int {
	for i, r := range models.AllResolutions {
		if r == res {
			return i
		}
	}
	return 0
}

func parseInt(v interface{}) int {
	switch val := v.(type) {
	case string:
		i, _ := strconv.Atoi(val)
		return i
	case int:
		return val
	case float64:
		return int(val)
	}
	return 0
}

func parseInt64(v interface{}) int64 {
	switch val := v.(type) {
	case string:
		i, _ := strconv.ParseInt(val, 10, 64)
		return i
	case int:
		return int64(val)
	case float64:
		return int64(val)
	}
	return 0
}
