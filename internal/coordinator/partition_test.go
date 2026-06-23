package coordinator

import (
	"testing"

	"github.com/distributed-transcoder/internal/models"
)

func TestParseSegmentKey(t *testing.T) {
	tests := []struct {
		key            string
		expectedSeg    int
		expectedRes    models.Resolution
	}{
		{
			key:         "jobs/partition_123/job_abc/transcoded/segment_003_1080p.ts",
			expectedSeg: 3,
			expectedRes: models.Res1080p,
		},
		{
			key:         "segment_010_720p.ts",
			expectedSeg: 10,
			expectedRes: models.Res720p,
		},
		{
			key:         "jobs/partition_45/job_xyz/transcoded/segment_000_480p.ts",
			expectedSeg: 0,
			expectedRes: models.Res480p,
		},
		{
			key:         "invalid_key_format.mp4",
			expectedSeg: 0,
			expectedRes: models.Res1080p,
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			seg, res := parseSegmentKey(tt.key)
			if seg != tt.expectedSeg {
				t.Errorf("expected segment %d, got %d", tt.expectedSeg, seg)
			}
			if res != tt.expectedRes {
				t.Errorf("expected resolution %s, got %s", tt.expectedRes, res)
			}
		})
	}
}
