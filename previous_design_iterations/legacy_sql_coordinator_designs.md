# Archived Legacy SQL-Centric Coordinator Designs & Research

This document preserves the database-centric (PostgreSQL tasks database) system design, Low-Level Go implementations, failure recovery protocols, and race-condition audits developed during the initial design phases before transitioning to a 100% decentralized, shared-nothing architecture.

---

## 1. Legacy Detailed Coordinator Design (active_active_coordinator_design.md)

### Detailed Design: Active-Active Coordinator Plane & Fault Tolerance

This document provides the systems engineering design, low-level architecture, database schemas, failure recovery flows, and race-condition mitigations for the database-centric Active-Active Coordinator Plane in the Distributed Video Transcoding system.

#### High-Level Design (HLD) & Sharding Mechanics

At a scale of 5M to 10M jobs, a single active leader coordinator will experience CPU, RAM, and network I/O bottlenecks. To scale horizontally, the system runs all coordinator nodes in an **Active-Active** configuration, sharding jobs deterministically via a **Consistent Hash Ring**.

##### System Architecture Diagram
```
                          ┌───────────────────────┐
                          │   Client App Upload   │
                          └───────────┬───────────┘
                                      │ (1. Multipart Upload)
                                      ▼
                          ┌───────────────────────┐
                          │    S3 Object Store    │
                          └───────────┬───────────┘
                                      │ (2. ObjectCreated Event)
                                      ▼
                    ┌──────────────────────────────────┐
                    │     NATS JetStream (Message Bus) │
                    │   - job-uploads.partition.<id>   │
                    │   - task-updates.partition.<id>  │
                    └─────────┬───────────────┬────────┘
                               │               │
                               ▼ (3. Route)    ▼ (3. Route)
                         (Partition A)   (Partition B)
                    ┌─────────────────┐ ┌─────────────────┐
                    │  Coordinator 1  │ │  Coordinator 2  │
                    │  (Active Task)  │ │  (Active Task)  │
                    └────────┬────────┘ └────────┬────────┘
                             │                   │
                             ▼ (4. CAS Write)    ▼ (4. CAS Write)
                    ┌─────────────────────────────────────┐
                    │          PostgreSQL Database        │
                    │       - idx_jobs_partition_status   │
                    └─────────────────────────────────────┘
                                       ▲
                                       │ (5. Watch registry & ring_rev)
                                [ etcd Cluster ]
```

##### Component Roles
1.  **Ingest API Gateway**: Stateless HTTP gateway. Receives job requests, queries `etcd` for healthy coordinators, hashes the `Job_UUID`, and resolves the owner coordinator on the ring.
2.  **Active-Active Coordinators**: Concurrently active nodes. Each node schedules tasks, monitors workers, and compiles HLS manifests **only** for the subset of jobs it owns.
3.  **Consensus Directory (etcd)**: Maintains coordinator node registry (`/registry/coordinators/{coord_id}`) via ephemeral leases (TTL: 5s). Broadcasts node join/leave events.

##### Step-by-Step Operations

###### Job Initialization & Sharding
1.  Client initiates an upload. Ingest Gateway generates a `Job_UUID`.
2.  The gateway hashes the `Job_UUID` to determine the partition slot:
    $$\text{partition\_id} = \text{FNV-1a}(\text{Job\_UUID}) \pmod{1024}$$
    **CRITICAL**: Both the Ingest Gateway hashing algorithm and NATS' built-in `{{hash}}` function must use the exact same hash function (e.g. FNV-1a modulo 1024 or Murmur3) to ensure a given `Job_UUID` maps to the exact same partition slot across the gateway database routing and NATS subject mapping transforms.
3.  The gateway retrieves the owner coordinator from its local, pre-computed Consistent Hash Ring partition slot mapping. (The mapping is cached locally by gateways/coordinators and resolved in $O(1)$ lookup complexity, recalculated only when a watch event indicates membership changes in `etcd`).
4.  It writes the job directly to the PostgreSQL primary node, setting the `owner_coordinator_id` to the resolved node, `owner_epoch = 0`, `partition_id = partition_id`, and the `ring_revision` value fetched from `etcd`. **CRITICAL**: To prevent replication lag anomalies, all subsequent coordinator reads and writes tracking job or task status transitions must run directly on the PostgreSQL primary (master) node to ensure strong consistency.
5.  It publishes an S3 upload complete event to a partition-scoped NATS subject: `job-uploads.partition.<partition_id>.job.<job_uuid>`, ensuring exactly-once delivery directly to the owner coordinator without queue group NAK storms.

###### Divided Task Scheduling & Progress Updates
*   **Decoupled Queuing**: Workers pull transcoding tasks from a single global NATS JetStream queue: `transcode-tasks`. The task payload contains the `job_id` and the coordinator's current `owner_epoch`.
*   **Targeted Status Updates**: Workers publish status updates to a partition-scoped NATS subject: `task-updates.partition.<partition_id>.job.<job_uuid>`. The owner coordinator is subscribed to the wildcard stream for the partitions it currently owns (`task-updates.partition.<partition_id>.job.*`), updating PostgreSQL and caching manifests. This isolates message routing and bounds NATS consumer scaling to a constant slot size.

---

#### Low-Level Design (LLD)

##### Database & Distributed State Schema

###### PostgreSQL Schema (Metadata & Fencing Tokens)
```sql
CREATE TYPE job_status AS ENUM ('QUEUED', 'CHUNKING', 'PROCESSING', 'STITCHING', 'COMPLETED', 'FAILED');
CREATE TYPE task_status AS ENUM ('PENDING', 'ASSIGNED', 'COMPLETED', 'FAILED');

CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    raw_video_url TEXT NOT NULL,
    status job_status NOT NULL DEFAULT 'QUEUED',
    target_profiles JSONB NOT NULL,
    owner_coordinator_id VARCHAR(100) NOT NULL,
    owner_epoch INT NOT NULL DEFAULT 0,    -- Fencing token
    ring_revision INT NOT NULL DEFAULT 0,  -- Hash ring revision key
    partition_id INT NOT NULL DEFAULT 0,   -- Hash partition slot (0 to 1023)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_jobs_partition_status ON jobs(partition_id, status) 
WHERE status IN ('CHUNKING', 'PROCESSING', 'STITCHING');


CREATE TABLE tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID REFERENCES jobs(id) ON DELETE CASCADE,
    segment_index INT NOT NULL,
    resolution VARCHAR(10) NOT NULL,
    codec VARCHAR(10) NOT NULL,
    raw_chunk_url TEXT NOT NULL,
    status task_status NOT NULL DEFAULT 'PENDING',
    assigned_worker_id VARCHAR(100),
    attempts INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(job_id, segment_index, resolution)
);

CREATE INDEX idx_tasks_active_recovery ON tasks(job_id) 
WHERE status = 'ASSIGNED';
```

###### etcd KV Key Structures
```
/registry/coordinators/{coord_id}               -> {"host": "10.0.0.1", "hash": 128491823} (Lease TTL: 5s)
/leases/partition/{partition_id}/task/{task_id} -> WorkerID (Lease TTL: 30s)
```

##### Shard Handover & Failover Loop (Go)
```go
package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ShardHandoverDaemon struct {
	ID      string
	EtcdCli EtcdClusterClient
	StateDB Database
	DB      DBStore
	Nats    NatsClient
}

func (d *ShardHandoverDaemon) MonitorClusterMembership(ctx context.Context) {
	var lastRev int64
	watchChan := d.EtcdCli.WatchPrefix(ctx, "/registry/coordinators/", 0)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watchChan:
			if !ok {
				select {
				case <-ctx.Done():
					return
				case <-time.After(1 * time.Second):
					watchChan = d.EtcdCli.WatchPrefix(ctx, "/registry/coordinators/", lastRev+1)
				}
				continue
			}

			if event.Revision > lastRev {
				lastRev = event.Revision
			}

			if event.Type == EventTypeDelete {
				crashedCoordID := extractCoordID(event.Key)
				if crashedCoordID == d.ID {
					continue
				}
				go d.executeTakeover(ctx, crashedCoordID)
			}
		}
	}
}

func (d *ShardHandoverDaemon) RunReconciliationPoller(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			onlineCoords, err := d.EtcdCli.GetKeysPrefix(ctx, "/registry/coordinators/")
			if err != nil {
				continue
			}

			onlineSet := make(map[string]bool)
			onlineCoordsList := make([]string, 0, len(onlineCoords))
			for _, entry := range onlineCoords {
				coordID := extractCoordID(entry.Key)
				onlineSet[coordID] = true
				onlineCoordsList = append(onlineCoordsList, coordID)
			}

			myOwnedPartitions := d.getMyOwnedPartitions()
			orphanedJobs, err := d.StateDB.GetActiveJobsWithOfflineOwners(ctx, myOwnedPartitions, onlineCoordsList)
			if err != nil {
				continue
			}

			jobsByCrashedOwner := make(map[string][]int)
			for _, job := range orphanedJobs {
				jobsByCrashedOwner[job.OwnerCoordinatorID] = append(jobsByCrashedOwner[job.OwnerCoordinatorID], job.PartitionID)
			}

			for crashedOwner, partitions := range jobsByCrashedOwner {
				ringRev, err := d.EtcdCli.GetRingRevision(ctx)
				if err == nil {
					_, _ = d.StateDB.AtomicTakeoverPartitions(ctx, crashedOwner, d.ID, ringRev, partitions)
				}
			}
		}
	}
}

func (d *ShardHandoverDaemon) executeTakeover(ctx context.Context, crashedCoordID string) {
	myNewPartitions := d.getNewPartitionsFromCrashedNode(crashedCoordID)
	if len(myNewPartitions) == 0 {
		return
	}

	ringRev, err := d.EtcdCli.GetRingRevision(ctx)
	if err != nil {
		return
	}

	_, err = d.StateDB.AtomicTakeoverPartitions(ctx, crashedCoordID, d.ID, ringRev, myNewPartitions)
	if err != nil {
		return
	}
}

func (d *ShardHandoverDaemon) getNewPartitionsFromCrashedNode(crashedCoordID string) []int { return nil }
func (d *ShardHandoverDaemon) getMyOwnedPartitions() []int                       { return nil }

func extractCoordID(key string) string {
	return strings.TrimPrefix(key, "/registry/coordinators/")
}

// System Interface Definitions
type EventType int
const (
	EventTypePut EventType = iota
	EventTypeDelete
)

type WatchEvent struct {
	Type     EventType
	Key      string
	Revision int64
}

type LeaseEntry struct {
	Key   string
	Value string
}

type Job struct {
	ID                 string
	OwnerCoordinatorID string
	OwnerEpoch         int
	RingRevision       int
	PartitionID        int
}

type Task struct {
	ID          string
	InputURL    string
	Resolution  string
	BitrateKbps int
}

type EtcdClusterClient interface {
	GetKeysPrefix(ctx context.Context, prefix string) ([]LeaseEntry, error)
	Exists(ctx context.Context, key string) (bool, error)
	WatchPrefix(ctx context.Context, prefix string, startRev int64) <-chan WatchEvent
	WatchKey(ctx context.Context, key string, startRev int64) <-chan WatchEvent
	GetRingRevision(ctx context.Context) (int, error)
}

type Database interface {
	GetActiveJobs() ([]Job, error)
	GetActiveJobsWithOfflineOwners(ctx context.Context, partitions []int, onlineCoords []string) ([]Job, error)
	GetUnfinishedTasks(ctx context.Context, jobID string) ([]Task, error)
	ResetTask(ctx context.Context, taskID string) error
	AtomicTakeoverPartitions(ctx context.Context, crashedCoord string, myID string, ringRev int, partitions []int) ([]Job, error)
}

type DBStore interface {
	UpdateJobOwner(ctx context.Context, jobID string, newCoord string, newEpoch int) error
}

type NatsClient interface {
	PublishTask(task Task, epoch int) error
}
```

#### Fault Tolerance & Failure Recovery Designs

##### Failure Scenario 1: Coordinator Node Crash Mid-Job
*   **The Risk**: Coordinator A crashes while scheduling tasks for `job_102`. Worker updates are orphaned, and the job freezes.
*   **The Recovery**:
    1.  Coordinator A's heartbeat lease in `etcd` expires. `/registry/coordinators/coord_A` is deleted.
    2.  Coordinator B detects the deletion event via its `etcd` Watcher, recalculates the ring, and identifies only the specific partition IDs it inherits from the crashed coordinator.
    3.  Coordinator B executes a single fast database transaction (`AtomicTakeoverPartitions`) to claim ownership of only its inherited partitions in PostgreSQL, updating `ring_revision` and incrementing `owner_epoch`. Because partitions are disjoint across coordinators B, C, and D, there is no database lock contention or etcd locks required.
    4.  Coordinator B subscribes to the NATS JetStream status updates for its newly adopted partitions. The tasks remain in the `ASSIGNED` state; the periodic reconciliation poller (Design A) will asynchronously verify worker leases in `etcd` and reschedule any orphaned tasks.
    5.  **Watcher Event Loss Mitigation**: If the watch channel disconnects, any coordinator deletions occurring during the downtime are missed by the reactive watcher. The 60-second periodic reconciliation poller acts as the ultimate safety net, sweeping active partitions and executing batch takeovers for any offline owner, ensuring eventual failover convergence.
    6.  **Interrupted Ingestion / Stitching Recovery**: If a coordinator crashes or is rebalanced while chunking (slicing) a video or stitching a manifest, the job will be orphaned in `CHUNKING` or `STITCHING` status. The 60-second periodic reconciliation daemon scans for jobs in these statuses with inactive epochs or those where no task updates have occurred for over 5 minutes. The adopting coordinator then re-initiates the slicing (using idempotent task inserts: `ON CONFLICT DO NOTHING` on unique keys) or manifest stitching process.

##### Failure Scenario 2: Coordinator Network Partition (Split-Brain)
*   **The Risk**: Coordinator A loses connection to `etcd` but remains connected to Postgres and NATS. It doesn't know Coordinator B took over its jobs, so both run writes concurrently.
*   **The Recovery (Double-Fencing)**:
    *   **Self-Fencing**: The coordinator's background keep-alive loop must renew its `etcd` lease (TTL: 5s) every 1.5s. If it fails to renew the lease within a grace threshold of **3 seconds**, it must immediately self-fence by halting all database writes and NATS consumer loops. This 2-second safety buffer guarantees the coordinator is fully silenced *before* any surviving coordinator can detect its lease deletion in `etcd` (at 5s) and start a takeover.
    *   **Database Fencing**: Any database write executed by the coordinator—including task updates and task registrations (INSERTs during slicing)—must check the `owner_epoch` and `owner_coordinator_id` in a `WHERE EXISTS` clause.
        ```sql
        UPDATE tasks SET status = 'COMPLETED' 
        WHERE id = $1 AND EXISTS (
            SELECT 1 FROM jobs WHERE id = tasks.job_id 
            AND owner_coordinator_id = 'coord_A' AND owner_epoch = 15
        );
        ```

##### Failure Scenario 3: Worker Node Crashes Mid-Transcode
*   **The Risk**: A worker crashes or OOMs while transcoding segment 15. The task remains `ASSIGNED` in the database indefinitely.
*   **The Recovery**:
    1.  **Active Lease Keep-Alives**: The worker's task lease at `/leases/partition/{partition_id}/task/{task_id}` in `etcd` is bound to an active keep-alive channel with a TTL of 30 seconds. The worker must actively renew this lease every 10 seconds via a background heartbeat channel. If the worker crashes, OOMs, or becomes unresponsive, the heartbeat stops and the lease automatically expires.
    2.  **Worker CPU Starvation & Self-Fencing Watchdog**:
        - To prevent CPU-intensive FFmpeg transcoding from starving the worker's Go agent heartbeat scheduler, the agent runs the `ffmpeg` subprocess under low priority (e.g. using `nice -n 10` or constrained to specific CPU cores via OS `cgroups`).
        - The Go agent's watchdog loop runs on a dedicated system thread via `runtime.LockOSThread()`. If it fails to reach `etcd` to renew its task lease and the lease is within 10 seconds of expiration (the safety grace window), the watchdog must immediately issue a `SIGKILL` to the running `ffmpeg` subprocess, discard the task, and clean up local scratch directories. This guarantees that a partitioned worker stops executing before the coordinator can reschedule the task.
    3.  **Decoupled Periodic Rescheduling (Design A)**: To prevent race conditions between NATS task updates and etcd events at high scale, the coordinator does not use a reactive etcd watcher for lease deletions. Instead, the coordinator's 60s periodic reconciliation daemon sweeps the database for any `ASSIGNED` tasks.
    4.  **Partitioned etcd Prefix Scan Optimization**: Instead of running a single global scan across all active tasks (which would return 100k+ keys and crash etcd/network capacity), the coordinator only issues prefix scans for the specific subset of the 1024 partitions it currently owns: `/leases/partition/{partition_id}/task/`. This reduces etcd scan payloads by ~75% and scales horizontally as nodes are added.
    5.  **Local Memory Lookup**: The coordinator constructs a local hash set of active leases for its owned partitions. To avoid scanning tasks belonging to other partitions, the coordinator sweeps active tasks using an inner JOIN with the `jobs` table, filtering strictly by the coordinator's owned partitions and utilizing the partial indexes `idx_tasks_active_recovery` and `idx_jobs_partition_status` to complete in $<1\text{ms}$:
        ```sql
        SELECT t.id, t.job_id FROM tasks t
        JOIN jobs j ON t.job_id = j.id
        WHERE t.status = 'ASSIGNED' 
          AND j.partition_id IN (12, 45, 99) 
          AND j.owner_coordinator_id = 'coord_D';
        ```
        For each retrieved task, the daemon verifies if the task's ID exists in the active lease set. If the lease is missing, the coordinator atomically increments the task's `attempts` counter, resets its status to `PENDING` in PostgreSQL, and republishes the task payload to NATS JetStream, ensuring reliable worker recovery with near-zero overhead.

##### Failure Scenario 4: NATS Queue Broker Crashes
*   **The Risk**: The message broker hosting the `transcode-tasks` queue crashes while millions of tasks are in flight.
*   **The Recovery**: NATS JetStream operates in a Raft consensus cluster (usually 3 or 5 nodes). The remaining nodes elect a new leader in <1 second. Replicated write-ahead logs on disk recover all unacknowledged tasks, re-delivering them to workers.

##### PostgreSQL Master Database Crashes
*   **The Risk**: The central database goes offline, preventing updates.
*   **The Recovery**: PostgreSQL HA managed by Patroni failover. Promotes standby in <10s. Connections shift automatically via pgBouncer / HAProxy or Keepalived Virtual IP (VIP) to the newly promoted primary.

##### Adding New Coordinator / Scale-Up & Rebalancing Policy
1.  **Registration & Ring Update**: Coordinator joins, registers `/registry/coordinators/coord_D` in etcd. Bumps `ring_revision`.
2.  **Ring Re-Calculation**: All nodes recalculate the Consistent Hash Ring.
3.  **Graceful Handover Offloading (Old Owners)**: Terminating or offloaded nodes unsubscribe from partition-scoped NATS subjects, drop transient in-flight updates, and complete manifest stitching in memory.
4.  **Atomic Adoption & Fencing (New Owner)**: Adopting coordinator binds to NATS durable consumers and executes `AtomicTakeoverPartitions` in PostgreSQL using a `ring_revision <= $1` fencing clause.

---

#### Active-Active Distributed Bug Resolutions

##### 4.1 NATS "Dead-Letter" Status Update & Metadata Scale-Up (Bug 1 Resolution)
Virtual Partition slots. Hash space divided into 1024 fixed partitions: `partition_id = Hash(Job_UUID) % 1024`. Durable consumers pre-created at deployment to avoid NATS consensus consensus Raft overloads. Coordinators bind to pre-provisioned consumers for the partitions they own.

##### 4.2 S3 Event Webhook Routing Bottleneck (Bug 2 Resolution)
S3 notifications write to a single global topic `s3-raw-uploads.job.<job_uuid>`. NATS native subject mapping sharding transform maps to:
`s3-raw-uploads.job.*  ->  job-uploads.partition.{{hash(1024, 1)}}.job.{{1}}`
Coordinators subscribe strictly to their partition slots, achieving exactly-once event routing.

##### 4.3 Split-Brain "TOCTOU" DB Fencing Race (Bug 3 Resolution)
1. **Epoch Fencing**: `owner_epoch` CAS checks.
2. **SELECT FOR SHARE Row Locks**: minimized to <5ms (no I/O in transactions).
3. **ACK/NAK Policy**: NAK on fenced writes, ACK on already-completed tasks.
4. **assigned_worker_id checks**:
   ```sql
   UPDATE tasks SET status = 'COMPLETED' 
   WHERE id = 'task_42' AND status = 'ASSIGNED' AND assigned_worker_id = 'worker_99'
     AND EXISTS (
         SELECT 1 FROM jobs 
         WHERE id = tasks.job_id 
           AND owner_coordinator_id = 'coord_A' 
           AND owner_epoch = 15
     );
   ```

##### 4.4 Consistent Hash Ring "Rebalance Storm" (Bug 4 Resolution)
Disjoint partitions takeover. Lockless concurrent takeovers using partial index `idx_jobs_partition_status` in under 10ms.

---
---

## 2. Legacy Master Architecture Plan (distributed_transcoder_design_plan.md)

Preserves the combined architectural design plan detailing the end-to-end ingest gateways, worker compute node execution blocks in Go (FFmpeg isolate subprocesses, watchdog locks), database clustering topographies, and the metadata task-state scheduling schemas prior to the shift to a database-less model.

*(The master plan was fully aligned to the above schemas, including the low-level worker code which used deferred NATS status updates and proactive etcd lease cleanups during runtime execution).*
