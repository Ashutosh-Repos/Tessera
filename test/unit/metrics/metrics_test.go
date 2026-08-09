package metrics_test

import (
	"testing"

	"github.com/distributed-transcoder/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

func TestNewGatewayMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.NewGatewayMetrics(reg)

	if m == nil {
		t.Fatalf("NewGatewayMetrics returned nil")
	}
	if m.UploadRequests == nil || m.UploadBytes == nil || m.ActiveWebSockets == nil || m.PresignedURLLatency == nil || m.RateLimitRejects == nil {
		t.Errorf("NewGatewayMetrics metrics fields contain nil pointers")
	}
}

func TestNewCoordinatorMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.NewCoordinatorMetrics(reg)

	if m == nil {
		t.Fatalf("NewCoordinatorMetrics returned nil")
	}
	if m.ActiveJobs == nil || m.SlicingBacklog == nil || m.SlicingDuration == nil || m.ManifestDuration == nil || m.BitcountLatency == nil || m.PartitionAdoptions == nil || m.DLQDepth == nil || m.GCOrphanedJobs == nil {
		t.Errorf("NewCoordinatorMetrics metrics fields contain nil pointers")
	}
}

func TestNewWorkerMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.NewWorkerMetrics(reg)

	if m == nil {
		t.Fatalf("NewWorkerMetrics returned nil")
	}
	if m.TranscodeDuration == nil || m.FFmpegCrashes == nil || m.IdempotencyHits == nil || m.S3FallbackTotal == nil || m.CircuitBreakerOpen == nil || m.DiskFreeBytes == nil || m.NATSInflightTasks == nil || m.GPUUtilization == nil {
		t.Errorf("NewWorkerMetrics metrics fields contain nil pointers")
	}
}
