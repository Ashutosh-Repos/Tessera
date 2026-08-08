package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSegmentTask_BitIndex(t *testing.T) {
	tests := []struct {
		name       string
		segmentIdx int
		resolution Resolution
		wantBit    int
	}{
		{"segment 0 1080p", 0, Res1080p, 0}, // 0 * 3 + 0
		{"segment 0 720p", 0, Res720p, 1},   // 0 * 3 + 1
		{"segment 0 480p", 0, Res480p, 2},   // 0 * 3 + 2
		{"segment 1 1080p", 1, Res1080p, 3}, // 1 * 3 + 0
		{"segment 1 720p", 1, Res720p, 4},   // 1 * 3 + 1
		{"segment 1 480p", 1, Res480p, 5},   // 1 * 3 + 2
		{"segment 10 1080p", 10, Res1080p, 30},
		{"segment 10 480p", 10, Res480p, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := SegmentTask{
				SegmentIdx: tt.segmentIdx,
				Resolution: tt.resolution,
			}
			got := task.BitIndex()
			if got != tt.wantBit {
				t.Errorf("task.BitIndex() = %d, want %d", got, tt.wantBit)
			}
		})
	}
}

func TestJobPhase_Constants(t *testing.T) {
	phases := []JobPhase{
		JobPhaseCreated,
		JobPhaseSlicing,
		JobPhaseTranscoding,
		JobPhaseCompiling,
		JobPhaseCompleted,
		JobPhaseFailed,
	}

	wantStrings := []string{
		"CREATED",
		"SLICING",
		"TRANSCODING",
		"COMPILING",
		"COMPLETED",
		"FAILED",
	}

	for i, phase := range phases {
		if string(phase) != wantStrings[i] {
			t.Errorf("JobPhase string = %q, want %q", phase, wantStrings[i])
		}
	}
}

func TestProgressUpdate_JSON(t *testing.T) {
	update := ProgressUpdate{
		Phase:      JobPhaseTranscoding,
		Completed:  10,
		Total:      30,
		Percent:    33,
		HLSURL:     "http://minio:9000/bucket/master.m3u8",
		DASHURL:    "http://minio:9000/bucket/manifest.mpd",
		Error:      "",
		Duration:   120.5,
		Thumbnails: []string{"thumb0.jpg", "thumb1.jpg"},
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("json.Marshal(ProgressUpdate) failed: %v", err)
	}

	var decoded ProgressUpdate
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(ProgressUpdate) failed: %v", err)
	}

	if decoded.Phase != update.Phase || decoded.Completed != update.Completed || decoded.Total != update.Total {
		t.Errorf("decoded ProgressUpdate = %+v, want %+v", decoded, update)
	}
}

func TestUploadSessionClaims_JSON(t *testing.T) {
	now := time.Now()
	claims := UploadSessionClaims{
		JobID:    "us-east:job-123",
		UploadID: "upload-id-456",
		Bucket:   "transcoder-bucket",
		Key:      "jobs/partition_1/job_123/raw/source.mp4",
	}

	data, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal(UploadSessionClaims) failed: %v", err)
	}

	var decoded UploadSessionClaims
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(UploadSessionClaims) failed: %v", err)
	}

	if decoded.JobID != claims.JobID || decoded.UploadID != claims.UploadID || decoded.Bucket != claims.Bucket || decoded.Key != claims.Key {
		t.Errorf("decoded UploadSessionClaims = %+v, want %+v", decoded, claims)
	}
	_ = now
}
