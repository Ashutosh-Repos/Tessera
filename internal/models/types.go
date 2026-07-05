package models

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ──────────────── Job Lifecycle ────────────────

type JobPhase string

const (
	JobPhaseCreated     JobPhase = "CREATED"
	JobPhaseSlicing     JobPhase = "SLICING"
	JobPhaseTranscoding JobPhase = "TRANSCODING"
	JobPhaseCompiling   JobPhase = "COMPILING"
	JobPhaseCompleted   JobPhase = "COMPLETED"
	JobPhaseFailed      JobPhase = "FAILED"
)

type Resolution string

const (
	Res1080p Resolution = "1080p"
	Res720p  Resolution = "720p"
	Res480p  Resolution = "480p"
)

var AllResolutions = []Resolution{Res1080p, Res720p, Res480p}

// JobManifest is written to S3 as job_manifest.json.
// It is the contract between Gateway → Coordinator → Worker.
type JobManifest struct {
	JobID        string       `json:"job_id"`
	PartitionID  int          `json:"partition_id"`
	OwnerEpoch   int64        `json:"owner_epoch"`
	Region       string       `json:"region"`
	SourcePath   string       `json:"source_path"` // S3 key of raw upload
	SourceSizeB  int64        `json:"source_size_bytes"`
	SourceCodec  string       `json:"source_codec"` // e.g. "h264"
	SourceFPS    float64      `json:"source_fps"`
	DurationSec  float64      `json:"duration_sec"`
	Resolutions  []Resolution `json:"resolutions"`   // target outputs
	SegmentCount int          `json:"segment_count"` // populated after slicing
	TotalTasks   int          `json:"total_tasks"`   // segment_count × len(resolutions)
	CreatedAt    time.Time    `json:"created_at"`
}

// JobStatus maps to Redis HASH `job:{uuid}:status`.
type JobStatus struct {
	JobID       string   `json:"job_id"       redis:"job_id"`
	Phase       JobPhase `json:"phase"        redis:"state"`
	Completed   int      `json:"completed"    redis:"completed"`
	Total       int      `json:"total"        redis:"total"`
	OwnerEpoch  int64    `json:"owner_epoch"  redis:"owner_epoch"`
	PartitionID int      `json:"partition_id" redis:"partition"`
	LastUpdated int64    `json:"last_updated" redis:"last_updated"` // unix timestamp
}

// ──────────────── Task Dispatch ────────────────

// SegmentTask is the NATS message payload published by coordinators
// and consumed by workers.
type SegmentTask struct {
	JobID       string     `json:"job_id"`
	PartitionID int        `json:"partition_id"`
	OwnerEpoch  int64      `json:"owner_epoch"`
	SegmentIdx  int        `json:"segment_idx"`
	Resolution  Resolution `json:"resolution"`
	RawChunkKey string     `json:"raw_chunk_key"` // S3 key: jobs/partition_{}/job_{}/raw/chunk_003.mp4
	OutputKey   string     `json:"output_key"`    // S3 key: jobs/partition_{}/job_{}/transcoded/segment_003_1080p.ts
	HWAccel     string     `json:"hw_accel"`      // hint: "nvenc" | "vaapi" | "videotoolbox" | "none"
	Priority    string     `json:"priority"`      // "high" | "normal" | "low"
}

// BitIndex computes the deterministic index into the progress bitmap.
// Formula: segment_index × len(AllResolutions) + resolution_offset
func (t *SegmentTask) BitIndex() int {
	offset := 0
	for i, r := range AllResolutions {
		if r == t.Resolution {
			offset = i
			break
		}
	}
	return t.SegmentIdx*len(AllResolutions) + offset
}

// ──────────────── Progress Updates ────────────────

// ProgressUpdate is the JSON payload written to Redis Streams
// (XADD progress:{uuid}) and forwarded via WebSocket.
type ProgressUpdate struct {
	Phase      JobPhase `json:"phase"`
	Completed  int      `json:"completed,omitempty"`
	Total      int      `json:"total,omitempty"`
	Percent    int      `json:"pct,omitempty"`
	HLSURL     string   `json:"hls_url,omitempty"`
	DASHURL    string   `json:"dash_url,omitempty"`
	Error      string   `json:"error,omitempty"`
	Sprite     string   `json:"sprite,omitempty"`
	SpriteVTT  string   `json:"sprite_vtt,omitempty"`
	Thumbnails []string `json:"thumbnails,omitempty"`
	Width      int      `json:"width,omitempty"`
	Height     int      `json:"height,omitempty"`
	FPS        int      `json:"fps,omitempty"`
	Duration   float64  `json:"duration,omitempty"`
}

// ──────────────── Upload Session ────────────────

// UploadSession is returned by POST /api/jobs/upload-session.
type UploadSession struct {
	JobID        string `json:"job_id"`
	SessionToken string `json:"session_token"` // JWT (24h expiry)
	UploadID     string `json:"upload_id"`     // S3 multipart upload ID
	PartSize     int64  `json:"part_size"`     // 50MB
	TotalParts   int    `json:"total_parts"`
	ProgressWSS  string `json:"progress_wss"` // wss://gateway/progress/{uuid}?token=...
}

// PresignedBatch is returned by POST /api/jobs/{uuid}/urls.
type PresignedBatch struct {
	PartNumbers []int    `json:"part_numbers"`
	URLs        []string `json:"urls"` // presigned PUT URLs (15 min expiry)
}

// CreateSessionRequest is the client payload for POST /api/jobs/upload-session.
type CreateSessionRequest struct {
	FileSizeBytes int64  `json:"file_size_bytes"` // total video file size
	FileName      string `json:"file_name"`       // original file name (for logs/metadata)
	ContentType   string `json:"content_type"`    // e.g., "video/mp4"
}

// UploadSessionClaims encodes the JWT claims issued to a client.
type UploadSessionClaims struct {
	jwt.RegisteredClaims
	JobID    string `json:"job_id"`
	UploadID string `json:"upload_id"` // S3 multipart upload ID
	Bucket   string `json:"bucket"`
	Key      string `json:"key"` // S3 object key
}
