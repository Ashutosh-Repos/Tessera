package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/models"
)

func Test_Models_PartitionOf_BoundaryPartitions(t *testing.T) {
	tests := []struct {
		jobID          string
		partitionCount int
	}{
		{"us-east-1:job-001", 1024},
		{"us-west-2:job-002", 512},
		{"eu-central-1:job-003", 256},
		{"ap-southeast-1:job-004", 64},
		{"sa-east-1:job-005", 1},
	}

	for _, tt := range tests {
		t.Run(tt.jobID, func(t *testing.T) {
			p := models.PartitionOf(tt.jobID, tt.partitionCount)
			if p < 0 || p >= tt.partitionCount {
				t.Errorf("PartitionOf(%s, %d) = %d out of bounds [0, %d)", tt.jobID, tt.partitionCount, p, tt.partitionCount)
			}
		})
	}
}

func Test_Models_JobManifest_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	manifest := models.JobManifest{
		JobID:        "us-east-1:test-job",
		PartitionID:  42,
		OwnerEpoch:   100,
		Region:       "us-east-1",
		SourcePath:   "raw/source.mp4",
		SourceSizeB:  1024 * 1024 * 500,
		SourceCodec:  "h264",
		SourceFPS:    29.97,
		DurationSec:  120.5,
		Resolutions:  []models.Resolution{models.Res1080p, models.Res720p},
		SegmentCount: 24,
		TotalTasks:   48,
		CreatedAt:    now,
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("failed to marshal JobManifest: %v", err)
	}

	var decoded models.JobManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal JobManifest: %v", err)
	}

	if decoded.JobID != manifest.JobID || decoded.PartitionID != 42 || decoded.TotalTasks != 48 {
		t.Errorf("manifest roundtrip mismatch: %+v vs %+v", decoded, manifest)
	}
}

func Test_Models_JobStatus_JSONRoundTrip(t *testing.T) {
	status := models.JobStatus{
		JobID:       "us-east-1:status-test",
		Phase:       models.JobPhaseTranscoding,
		Completed:   10,
		Total:       20,
		OwnerEpoch:  5,
		PartitionID: 7,
		LastUpdated: time.Now().Unix(),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal JobStatus: %v", err)
	}

	var decoded models.JobStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal JobStatus: %v", err)
	}

	if decoded.Phase != models.JobPhaseTranscoding || decoded.Completed != 10 {
		t.Errorf("status roundtrip mismatch: %+v vs %+v", decoded, status)
	}
}
