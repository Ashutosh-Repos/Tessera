package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/models"
	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client redis.UniversalClient
}

func NewRedisStore(cfg config.RedisConfig) (*RedisStore, error) {
	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:      cfg.Addrs,
		Password:   cfg.Password,
		MaxRetries: cfg.MaxRetries,
		PoolSize:   cfg.PoolSize,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return &RedisStore{client: client}, nil
}

func (r *RedisStore) SetJobStatus(ctx context.Context, jobID string, status map[string]interface{}) error {
	keys := NewRedisKeys(jobID)
	return r.client.HSet(ctx, keys.StatusHash(), status).Err()
}

func (r *RedisStore) GetJobStatus(ctx context.Context, jobID string) (map[string]string, error) {
	keys := NewRedisKeys(jobID)
	return r.client.HGetAll(ctx, keys.StatusHash()).Result()
}

func (r *RedisStore) IncrJobCompleted(ctx context.Context, jobID string) (int64, error) {
	keys := NewRedisKeys(jobID)
	return r.client.HIncrBy(ctx, keys.StatusHash(), "completed", 1).Result()
}

func (r *RedisStore) SetBit(ctx context.Context, jobID string, bitIdx int) error {
	keys := NewRedisKeys(jobID)
	return r.client.SetBit(ctx, keys.ProgressBitmap(), int64(bitIdx), 1).Err()
}

func (r *RedisStore) BitCount(ctx context.Context, jobID string) (int64, error) {
	keys := NewRedisKeys(jobID)
	return r.client.BitCount(ctx, keys.ProgressBitmap(), nil).Result()
}

func (r *RedisStore) TaskExists(ctx context.Context, jobID string, segment int, res string) (bool, error) {
	key := fmt.Sprintf("task:{%s}:%d:%s", jobID, segment, res)
	val, err := r.client.Exists(ctx, key).Result()
	return val > 0, err
}

func (r *RedisStore) SetTaskDone(ctx context.Context, jobID string, segment int, res string, ttl int) error {
	key := fmt.Sprintf("task:{%s}:%d:%s", jobID, segment, res)
	return r.client.Set(ctx, key, "1", time.Duration(ttl)*time.Second).Err()
}

func (r *RedisStore) SetSegmentDuration(ctx context.Context, jobID string, segRes string, duration string) error {
	keys := NewRedisKeys(jobID)
	return r.client.HSet(ctx, keys.DurationsHash(), segRes, duration).Err()
}

func (r *RedisStore) GetAllDurations(ctx context.Context, jobID string) (map[string]string, error) {
	keys := NewRedisKeys(jobID)
	return r.client.HGetAll(ctx, keys.DurationsHash()).Result()
}

func (r *RedisStore) AddActiveJob(ctx context.Context, partitionID int, jobID string) error {
	key := fmt.Sprintf("partition:%d:active_jobs", partitionID)
	return r.client.SAdd(ctx, key, jobID).Err()
}

func (r *RedisStore) RemoveActiveJob(ctx context.Context, partitionID int, jobID string) error {
	key := fmt.Sprintf("partition:%d:active_jobs", partitionID)
	return r.client.SRem(ctx, key, jobID).Err()
}

func (r *RedisStore) GetActiveJobs(ctx context.Context, partitionID int) ([]string, error) {
	key := fmt.Sprintf("partition:%d:active_jobs", partitionID)
	return r.client.SMembers(ctx, key).Result()
}

func (r *RedisStore) CacheManifest(ctx context.Context, jobID string, data []byte) error {
	keys := NewRedisKeys(jobID)
	return r.client.Set(ctx, keys.ManifestCache(), data, 24*time.Hour).Err()
}

func (r *RedisStore) GetCachedManifest(ctx context.Context, jobID string) ([]byte, error) {
	keys := NewRedisKeys(jobID)
	return r.client.Get(ctx, keys.ManifestCache()).Bytes()
}

func (r *RedisStore) PublishProgress(ctx context.Context, jobID string, update models.ProgressUpdate) error {
	keys := NewRedisKeys(jobID)
	args := &redis.XAddArgs{
		Stream: keys.ProgressStream(),
		Values: map[string]interface{}{
			"phase":     string(update.Phase),
			"completed": update.Completed,
			"total":     update.Total,
			"pct":       update.Percent,
			"hls_url":   update.HLSURL,
			"dash_url":  update.DASHURL,
			"error":     update.Error,
		},
	}
	return r.client.XAdd(ctx, args).Err()
}

func (r *RedisStore) ReadProgressStream(ctx context.Context, jobIDs []string, lastIDs []string, blockMs int) ([]StreamEntry, error) {
	streams := make([]string, 0, len(jobIDs)*2)
	for _, id := range jobIDs {
		keys := NewRedisKeys(id)
		streams = append(streams, keys.ProgressStream())
	}
	streams = append(streams, lastIDs...)

	args := &redis.XReadArgs{
		Streams: streams,
		Block:   time.Duration(blockMs) * time.Millisecond,
	}

	result, err := r.client.XRead(ctx, args).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}

	var entries []StreamEntry
	for _, stream := range result {
		// Extract jobID from stream name progress:{jobID}
		// stream.Stream is "progress:{uuid}"
		jobID := ""
		if len(stream.Stream) > 10 {
			jobID = stream.Stream[10 : len(stream.Stream)-1]
		}
		for _, msg := range stream.Messages {
			fields := make(map[string]string)
			for k, v := range msg.Values {
				if strVal, ok := v.(string); ok {
					fields[k] = strVal
				} else {
					fields[k] = fmt.Sprintf("%v", v)
				}
			}
			entries = append(entries, StreamEntry{
				JobID:  jobID,
				ID:     msg.ID,
				Fields: fields,
			})
		}
	}
	return entries, nil
}

func (r *RedisStore) DeduplicateEvent(ctx context.Context, jobID string) (bool, error) {
	key := fmt.Sprintf("dedup:event:{%s}", jobID)
	return r.client.SetNX(ctx, key, "1", 10*time.Minute).Result()
}

func (r *RedisStore) IncrRateLimit(ctx context.Context, key string, windowSec int) (int64, error) {
	// Atomic INCR + EXPIRE via pipeline
	pipe := r.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, time.Duration(windowSec)*time.Second)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

func (r *RedisStore) ExecuteCompletionPipeline(ctx context.Context, p CompletionPipelineParams) error {
	keys := NewRedisKeys(p.JobID)
	taskKey := fmt.Sprintf("task:{%s}:%d:%s", p.JobID, p.SegmentIdx, p.Resolution)
	segRes := fmt.Sprintf("%d_%s", p.SegmentIdx, p.Resolution)

	pct := int((float64(p.Completed) / float64(p.Total)) * 100)

	pipe := r.client.Pipeline()

	// 1. Mark task complete
	pipe.Set(ctx, taskKey, "1", 24*time.Hour)

	// 2. Set bit
	pipe.SetBit(ctx, keys.ProgressBitmap(), int64(p.BitIndex), 1)

	// 3. Update completion count
	pipe.HIncrBy(ctx, keys.StatusHash(), "completed", 1)
	pipe.HSet(ctx, keys.StatusHash(), "last_updated", p.UnixNow)

	// 4. Save duration
	pipe.HSet(ctx, keys.DurationsHash(), segRes, p.Duration)

	// 5. Emit progress
	pipe.XAdd(ctx, &redis.XAddArgs{
		Stream: keys.ProgressStream(),
		Values: map[string]interface{}{
			"phase":     string(models.JobPhaseTranscoding),
			"completed": p.Completed,
			"total":     p.Total,
			"pct":       pct,
		},
	})

	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) DeleteKeys(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return r.client.Del(ctx, keys...).Err()
}

func (r *RedisStore) ExpireJobKeys(ctx context.Context, jobID string, ttlSec int) error {
	keys := NewRedisKeys(jobID)
	pipe := r.client.Pipeline()
	ttl := time.Duration(ttlSec) * time.Second
	pipe.Expire(ctx, keys.StatusHash(), ttl)
	pipe.Expire(ctx, keys.ProgressBitmap(), ttl)
	pipe.Expire(ctx, keys.DurationsHash(), ttl)
	pipe.Expire(ctx, keys.ManifestCache(), ttl)
	pipe.Expire(ctx, keys.ProgressStream(), ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStore) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close releases the Redis connection pool.
func (r *RedisStore) Close() error {
	return r.client.Close()
}
