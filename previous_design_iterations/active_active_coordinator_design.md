# Detailed Design: 100% Decentralized Active-Active Coordinator Plane (Shared-Nothing)

This document specifies the systems engineering design, low-level architecture, state reconstruction protocols, and fault-tolerance mechanics for the **100% Decentralized, Shared-Nothing Active-Active Coordinator Plane**. This design completely eliminates centralized relational databases for job/task tracking, replacing them with event-driven object-store metadata and message queues to scale infinitely to 10M+ concurrent jobs.

---

## 1. Architectural Philosophy: Shared-Nothing (SN)

At a scale of 10M concurrent transcoding jobs (representing 30B+ individual segment tasks), centralized databases (even with primary-standby clustering) suffer write buffer saturation, row-lock contentions, and replication lag bottlenecks. 

This architecture adopts a **Shared-Nothing (SN)** model:
*   **Database-Less State Tracking**: There is no central PostgreSQL database or SQL transactions. 
*   **S3 as the Metadata Registry & State Store**: The physical existence of files (configuration manifests, transcoded segment chunks, progress logs) in the Distributed Object Store (S3) serves as the absolute single source of truth for job existence and task completion.
*   **NATS JetStream as the In-Flight State Store**: Active, pending, and in-flight tasks exist strictly as durable messages on NATS JetStream. 
*   **Independent Partition Owners**: Coordinators operate as independent entities sharded across a Consistent Hash Ring, managing task scheduling and progress tracking purely in-memory (and cached locally) for their owned partitions.

---

## 2. High-Level Design (HLD) & Distributed Data Flow

### 2.1 System Architecture Diagram
```
                          ┌────────────────────────┐
                          │   Client App Upload    │
                          └───────────┬────────────┘
                                      │ (1. Multipart Upload raw MP4)
                                      ▼
                          ┌────────────────────────┐
                          │    S3 Object Store     │
                          │   - raw-uploads/       │
                          │   - jobs/<id>/manifest │ (Single source of truth)
                          └───────────┬────────────┘
                                      │ (2. ObjectCreated notification)
                                      ▼
                    ┌──────────────────────────────────┐
                    │     NATS JetStream (Message Bus) │
                    │   - s3-raw-uploads.job.*         │
                    └─────────────────┬────────────────┘
                                      │ (3. Native Hashing Transform)
                                      ▼
                    ┌──────────────────────────────────┐
                    │   job-uploads.partition.<id>     │
                    └─────────┬───────────────┬────────┘
                               │               │
                               ▼ (4. Route)    ▼ (4. Route)
                         (Partition A)   (Partition B)
                    ┌─────────────────┐ ┌─────────────────┐
                    │  Coordinator 1  │ │  Coordinator 2  │
                    │ (Subscribes A)  │ │ (Subscribes B)  │
                    └────────┬────────┘ └────────┬────────┘
                             │                   │
             (5. Slice task  │                   │ (5. Slice task
              & NATS publish)│                   │  & NATS publish)
                             ▼                   ▼
                    ┌─────────────────────────────────────┐
                    │     Global NATS JetStream Queue     │
                    │   - transcode-tasks (Worker Pull)   │
                    └─────────────────────────────────────┘
```

### 2.2 Component Roles
1.  **Ingest API Gateway**: A stateless ingress proxy. Generates S3 presigned URLs, creates a `job_manifest.json` file directly in S3, and exits. It holds no routing or hashing caches.
2.  **Active-Active Coordinators**: Sharded nodes mapped to a Consistent Hash Ring. They subscribe strictly to partition-scoped NATS subjects for the partitions they own. They manage in-memory task state tracking and compile final HLS manifests.
3.  **Consensus Registry (etcd)**: Tracks active coordinator membership. Broadcasts ring revisions.
4.  **Distributed Object Store (S3)**: Serves as the global persistent storage. It hosts raw inputs, segment transcodes, manifest maps, and final HLS playlists.

### 2.3 Step-by-Step Operations

#### 2.3.1 Job Ingestion & Dynamic Adoption
1.  The client requests an upload. Ingest Gateway generates a `Job_UUID`.
2.  The gateway writes a `job_manifest.json` file directly to S3 under `s3://bucket/jobs/<job_uuid>/job_manifest.json` containing target profiles (resolutions, bitrates) and the hashed `partition_id` (determined via FNV-1a or Murmur3 modulo 1024).
3.  Writing the manifest triggers an S3 `ObjectCreated` event, published to NATS: `s3-raw-uploads.job.<job_uuid>`.
4.  **NATS Native Subject Mapping**: NATS automatically hashes the `job_uuid` at wire-speed and transforms the event:
    `s3-raw-uploads.job.*  ->  job-uploads.partition.{{hash(1024, 1)}}.job.{{1}}`
5.  The coordinator currently owning that partition slot on the consistent hash ring receives the event. It downloads the S3 `job_manifest.json` file and adopts the job, initializing an in-memory job tracker.

#### 2.3.2 Video Slicing & Task Dispatch
1.  The owner coordinator streams the raw input video from S3 and performs physical slicing (segmenting).
2.  For every generated segment and target profile, the coordinator publishes a transcoding task payload directly to NATS JetStream: `transcode-tasks.job.<job_uuid>`. The payload contains `JobID`, `TaskID`, `SegmentIndex`, `Resolution`, and the coordinator's current `OwnerEpoch`.

#### 2.3.3 Task Execution & Decentralized Commit
1.  A worker node pulls a task from the `transcode-tasks` queue, writes a lease key `/leases/partition/{partition_id}/task/{task_id}` to `etcd`, and transcodes the chunk.
2.  The worker uploads the output `.ts` chunk directly to S3:
    `s3://bucket/jobs/<job_uuid>/transcoded/segment_<index>_<resolution>.ts`
3.  Upon successful upload, the worker:
    *   Deletes its lease key from `etcd`.
    *   Publishes a task completion event containing the task metadata and S3 path to:
        `task-updates.partition.<partition_id>.job.<job_uuid>`.

#### 2.3.4 Manifest Compilation & Job Completion
1.  The owner coordinator receives the NATS completion event and marks the segment as completed in its local in-memory tracker.
2.  To guarantee crash resilience without writing task records to a database, the coordinator periodically flushes a lightweight `progress.json` status map to S3 (e.g. every 10 finished segments) or appends them to a local write-ahead log.
3.  Once the local tracker indicates all segment combinations are completed, the coordinator:
    *   Compiles the HLS master and media playlist manifests (`.m3u8`).
    *   Writes the manifests directly to S3.
    *   Writes a final `job_completed.json` file to S3, signaling end-to-end completion.

---

## 3. Low-Level Design (LLD) & State Reconstruction

### 3.1 Consistent Hash Ring & Partition Handovers
Coordinator nodes register their presence in `etcd` under `/registry/coordinators/{coord_id}` with a 5-second TTL lease. All nodes watch this path. When a membership change occurs, they recalculate their owned partitions.

#### 3.1.1 Graceful Handover Protocol
When Coordinator A detects it has lost partition 12 to Coordinator D:
1.  Coordinator A synchronously unsubscribes from `task-updates.partition.12.job.*`.
2.  It drops or ignores any in-flight messages for partition 12 received during unsubscription.
3.  It flushes its in-memory progress maps for partition 12 jobs to their respective S3 directories as `progress_handover.json`.
4.  It purges the partition 12 tracker from memory.

#### 3.1.2 Rebalancing Storm Mitigation (Flapping Grace Period)
To prevent network hiccups from causing immediate partition takeovers and NATS reconnect storms:
*   When a coordinator detects a node deletion in `etcd`, it delays taking over the inherited partitions for a **10-second grace window**.
*   If the departed node recovers and renews its registry key in `etcd` within this 10-second window, the rebalance is cancelled, completely avoiding rebalance storms.

---

### 3.2 State Reconstruction Daemon (Go)

When Coordinator D adopts a partition, it must reconstruct the progress of all active jobs in that partition from S3 and NATS. It does not read any SQL database.

```go
package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type S3Client interface {
	ListObjectsPrefix(ctx context.Context, prefix string) ([]string, error)
	DownloadFile(ctx context.Context, key string) ([]byte, error)
	UploadFile(ctx context.Context, key string, data []byte) error
}

type NatsClient interface {
	BindConsumer(ctx context.Context, partitionID int) (JetStreamSubscription, error)
}

type JetStreamSubscription interface {
	Unsubscribe() error
}

type JobProgress struct {
	JobID             string          `json:"job_id"`
	PartitionID       int             `json:"partition_id"`
	TotalSegments     int             `json:"total_segments"`
	Resolutions       []string        `json:"resolutions"`        // e.g. ["1080p", "720p", "480p"]
	TotalTasks        int             `json:"total_tasks"`         // = TotalSegments × len(Resolutions)
	CompletedSegments map[string]bool `json:"completed_segments"` // key: "<segment_index>_<resolution>"
}

type ShardAdoptionDaemon struct {
	ID        string
	S3Cli     S3Client
	NatsCli   NatsClient
	ActiveJobs map[string]*JobProgress
}

// ReconstructPartitionState rebuilds progress purely from S3 sidecar files and deterministic paths
func (s *ShardAdoptionDaemon) ReconstructPartitionState(ctx context.Context, partitionID int) error {
	// 1. List active jobs in the adopted partition by scanning the S3 prefix
	// Jobs are structured as: jobs/partition_<partition_id>/job_<job_uuid>/
	jobDirs, err := s.S3Cli.ListObjectsPrefix(ctx, fmt.Sprintf("jobs/partition_%d/", partitionID))
	if err != nil {
		return fmt.Errorf("failed to list partition jobs from S3: %w", err)
	}

	for _, dir := range jobDirs {
		jobID := extractJobID(dir)
		
		// Skip completed jobs
		if s.isJobCompleted(ctx, jobID, partitionID) {
			continue
		}

		// 2. Download manifest file
		manifestBytes, err := s.S3Cli.DownloadFile(ctx, fmt.Sprintf("jobs/partition_%d/job_%s/job_manifest.json", partitionID, jobID))
		if err != nil {
			continue // Manifest not written yet
		}
		
		var manifest struct {
			TotalSegments int      `json:"total_segments"`
			Resolutions   []string `json:"resolutions"` // e.g. ["1080p","720p","480p"]
		}
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			continue
		}
		if len(manifest.Resolutions) == 0 {
			continue // Manifest incomplete
		}

		// 3. Scan S3 transcoded directory to find completed chunks
		transcodedChunks, err := s.S3Cli.ListObjectsPrefix(ctx, fmt.Sprintf("jobs/partition_%d/job_%s/transcoded/", partitionID, jobID))
		if err != nil {
			transcodedChunks = []string{}
		}

		progress := &JobProgress{
			JobID:             jobID,
			PartitionID:       partitionID,
			TotalSegments:     manifest.TotalSegments,
			Resolutions:       manifest.Resolutions,
			TotalTasks:        manifest.TotalSegments * len(manifest.Resolutions),
			CompletedSegments: make(map[string]bool),
		}

		// 4. Populated completed chunks from S3 directory listings (Single Source of Truth)
		for _, chunkPath := range transcodedChunks {
			chunkKey := extractChunkKey(chunkPath) // returns e.g. "12_1080p"
			progress.CompletedSegments[chunkKey] = true
		}

		// 5. Check if progress sidecar is available from previous owner offload
		handoverBytes, err := s.S3Cli.DownloadFile(ctx, fmt.Sprintf("jobs/partition_%d/job_%s/progress_handover.json", partitionID, jobID))
		if err == nil {
			var handover JobProgress
			if json.Unmarshal(handoverBytes, &handover) == nil {
				for k := range handover.CompletedSegments {
					progress.CompletedSegments[k] = true
				}
			}
		}

		s.ActiveJobs[jobID] = progress
	}

	// 6. Bind to NATS pre-provisioned durable JetStream consumer for the partition
	_, err = s.NatsCli.BindConsumer(ctx, partitionID)
	if err != nil {
		return fmt.Errorf("failed to bind NATS consumer for partition %d: %w", partitionID, err)
	}

	return nil
}

func (s *ShardAdoptionDaemon) isJobCompleted(ctx context.Context, jobID string, partitionID int) bool {
	_, err := s.S3Cli.DownloadFile(ctx, fmt.Sprintf("jobs/partition_%d/job_%s/job_completed.json", partitionID, jobID))
	return err == nil
}

func extractJobID(dir string) string {
	parts := strings.Split(dir, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "job_") {
			return strings.TrimPrefix(p, "job_")
		}
	}
	return ""
}

func extractChunkKey(path string) string {
	// path is jobs/partition_x/job_y/transcoded/segment_12_1080p.ts
	parts := strings.Split(path, "/")
	filename := parts[len(parts)-1]
	filename = strings.TrimPrefix(filename, "segment_")
	return strings.TrimSuffix(filename, ".ts")
}
```

---

## 4. Fault Tolerance & Failure Recoveries

### 4.1 Coordinator Crash (Zero State Loss)
*   **The Risk**: The coordinator tracking Job A crashes mid-transcode. The in-memory tracker is lost.
*   **The Recovery**:
    1.  The failed node's etcd lease expires. Remaining coordinators recalculate the Consistent Hash Ring.
    2.  The adopting node triggers `ReconstructPartitionState()`.
    3.  It lists S3 files to rebuild the progress maps in-memory, binds to NATS, and resumes tracking completions without duplicate transcoding.

### 4.2 NATS Queue Broker Crashes
*   **The Risk**: The message broker holding transcoding tasks crashes.
*   **The Recovery**: NATS JetStream operates in a Raft consensus cluster. Replicated write-ahead logs recover all unacknowledged tasks. Any redelivered tasks are checked by workers in etcd; if a task's lease is still active or its output `.ts` file already exists in S3, it is skipped or immediately ACKeyed.

### 4.3 Worker Crash & Heartbeat Expire
*   **The Recovery**: The worker Go watchdog watchdog thread priority cgroups configuration remains unchanged. If the worker agent cannot reach etcd for 20 seconds, it sends a `SIGKILL` to `ffmpeg` and exits. The coordinator reconciliation sweep scans the `/leases/partition/{partition_id}/task/` keys in etcd for its owned partitions. If a task message remains unacknowledged in NATS, and no lease key exists in etcd, NATS automatically triggers redelivery after its `ack_wait` timeout (configured to a conservative 60 seconds).
