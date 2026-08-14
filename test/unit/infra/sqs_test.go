package infra_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestSQSPartitionRouting_Logic(t *testing.T) {
	tests := []struct {
		subject       string
		expectedQueue string
	}{
		{
			subject:       "s3-raw-uploads.job.partition_3.job_123",
			expectedQueue: "transcoder-upload-events-partition_3",
		},
		{
			subject:       "s3-transcoded.job.partition_7.job_456",
			expectedQueue: "transcoder-completion-events-partition_7",
		},
		{
			subject:       "s3-raw-uploads.job.global",
			expectedQueue: "transcoder-upload-events",
		},
		{
			subject:       "s3-transcoded.job.global",
			expectedQueue: "transcoder-completion-events",
		},
		{
			subject:       "transcode-tasks-dlq",
			expectedQueue: "transcoder-dlq",
		},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			var targetQueue string
			if strings.Contains(tt.subject, "s3-raw-uploads") {
				parts := strings.Split(tt.subject, ".")
				targetQueue = "transcoder-upload-events"
				for _, p := range parts {
					if strings.HasPrefix(p, "partition_") {
						targetQueue = fmt.Sprintf("transcoder-upload-events-%s", p)
						break
					}
				}
			} else if strings.Contains(tt.subject, "s3-transcoded") {
				parts := strings.Split(tt.subject, ".")
				targetQueue = "transcoder-completion-events"
				for _, p := range parts {
					if strings.HasPrefix(p, "partition_") {
						targetQueue = fmt.Sprintf("transcoder-completion-events-%s", p)
						break
					}
				}
			} else {
				targetQueue = "transcoder-dlq"
			}

			if targetQueue != tt.expectedQueue {
				t.Errorf("for subject %q, got targetQueue %q, want %q", tt.subject, targetQueue, tt.expectedQueue)
			}
		})
	}
}

func TestSQSQueueName_PerPartition(t *testing.T) {
	partitionID := 42
	uploadQueue := fmt.Sprintf("transcoder-upload-events-partition_%d", partitionID)
	completionQueue := fmt.Sprintf("transcoder-completion-events-partition_%d", partitionID)

	if uploadQueue != "transcoder-upload-events-partition_42" {
		t.Errorf("expected upload queue 'transcoder-upload-events-partition_42', got %q", uploadQueue)
	}
	if completionQueue != "transcoder-completion-events-partition_42" {
		t.Errorf("expected completion queue 'transcoder-completion-events-partition_42', got %q", completionQueue)
	}
}
