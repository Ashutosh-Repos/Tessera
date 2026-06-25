package infra

import (
	"context"

	"github.com/distributed-transcoder/internal/models"
)

// StateStore abstracts Redis Cluster operations.
// All keys MUST include {job_uuid} as a Hash Tag to prevent CROSSSLOT errors.
type StateStore interface {
	// Job Status (HASH)
	SetJobStatus(ctx context.Context, jobID string, status map[string]interface{}) error
	GetJobStatus(ctx context.Context, jobID string) (map[string]string, error)
	IncrJobCompleted(ctx context.Context, jobID string) (int64, error)

	// Progress Bitmap
	SetBit(ctx context.Context, jobID string, bitIdx int) error
	BitCount(ctx context.Context, jobID string) (int64, error)

	// Task Idempotency
	TaskExists(ctx context.Context, jobID string, segment int, res string) (bool, error)
	SetTaskDone(ctx context.Context, jobID string, segment int, res string, ttl int) error

	// Segment Durations (HASH)
	SetSegmentDuration(ctx context.Context, jobID string, segRes string, duration string) error
	GetAllDurations(ctx context.Context, jobID string) (map[string]string, error)

	// Partition Index (SET)
	AddActiveJob(ctx context.Context, partitionID int, jobID string) error
	RemoveActiveJob(ctx context.Context, partitionID int, jobID string) error
	GetActiveJobs(ctx context.Context, partitionID int) ([]string, error)

	// Manifest Cache (STRING with TTL)
	CacheManifest(ctx context.Context, jobID string, data []byte) error
	GetCachedManifest(ctx context.Context, jobID string) ([]byte, error)

	// Progress Stream (STREAM)
	PublishProgress(ctx context.Context, jobID string, update models.ProgressUpdate) error
	ReadProgressStream(ctx context.Context, jobIDs []string, lastIDs []string, blockMs int) ([]StreamEntry, error)

	// Deduplication (SETNX with TTL)
	DeduplicateEvent(ctx context.Context, jobID string) (bool, error) // true = first time

	// Rate Limiting
	IncrRateLimit(ctx context.Context, key string, windowSec int) (int64, error)

	// Completion Pipeline (atomic, single RTT)
	ExecuteCompletionPipeline(ctx context.Context, p CompletionPipelineParams) error

	// Cleanup (used by GC daemon)
	DeleteKeys(ctx context.Context, keys ...string) error
	ExpireJobKeys(ctx context.Context, jobID string, ttlSec int) error

	// Health
	Ping(ctx context.Context) error

	// Job Listing
	ScanJobKeys(ctx context.Context) ([]string, error)

	// Worker registry
	RegisterWorker(ctx context.Context, workerID string, info map[string]interface{}, ttlSec int) error
	GetActiveWorkers(ctx context.Context) (map[string]map[string]string, error)
}

type CompletionPipelineParams struct {
	JobID      string
	SegmentIdx int
	Resolution string
	BitIndex   int
	Duration   string
	UnixNow    int64
	Completed  int // current completed count for progress stream
	Total      int
}

type StreamEntry struct {
	JobID  string
	ID     string // Redis Stream entry ID
	Fields map[string]string
}

// RedisKeys constructs all Redis keys for a given job UUID.
// All keys include {job_uuid} as a Hash Tag, ensuring they
// route to the same Redis Cluster shard for pipeline safety.
type RedisKeys struct {
	JobID string
}

func NewRedisKeys(jobID string) RedisKeys {
	return RedisKeys{JobID: jobID}
}

func (k RedisKeys) StatusHash() string {
	return "job:{" + k.JobID + "}:status"
}

func (k RedisKeys) ProgressBitmap() string {
	return "job:{" + k.JobID + "}:progress"
}

func (k RedisKeys) DurationsHash() string {
	return "job:{" + k.JobID + "}:durations"
}

func (k RedisKeys) ManifestCache() string {
	return "job:{" + k.JobID + "}:manifest"
}

func (k RedisKeys) ProgressStream() string {
	return "progress:{" + k.JobID + "}"
}
