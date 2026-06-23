package metrics

import "github.com/prometheus/client_golang/prometheus"

// GatewayMetrics contains all Prometheus metrics for the Gateway tier.
type GatewayMetrics struct {
	UploadRequests      prometheus.Counter
	UploadBytes         prometheus.Counter
	ActiveWebSockets    prometheus.Gauge
	PresignedURLLatency prometheus.Histogram
	RateLimitRejects    prometheus.Counter
}

func NewGatewayMetrics(reg prometheus.Registerer) *GatewayMetrics {
	m := &GatewayMetrics{
		UploadRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gateway_upload_requests_total",
		}),
		UploadBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gateway_upload_bytes_total",
		}),
		ActiveWebSockets: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gateway_active_websockets",
		}),
		PresignedURLLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "gateway_presigned_url_latency_ms",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 200, 500},
		}),
		RateLimitRejects: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gateway_rate_limit_rejections_total",
		}),
	}
	reg.MustRegister(m.UploadRequests, m.UploadBytes, m.ActiveWebSockets,
		m.PresignedURLLatency, m.RateLimitRejects)
	return m
}

// CoordinatorMetrics contains all Prometheus metrics for the Coordinator tier.
type CoordinatorMetrics struct {
	ActiveJobs         prometheus.Gauge
	SlicingBacklog     prometheus.Gauge
	SlicingDuration    prometheus.Histogram
	ManifestDuration   prometheus.Histogram
	BitcountLatency    prometheus.Histogram
	PartitionAdoptions prometheus.Counter
	DLQDepth           prometheus.Gauge
	GCOrphanedJobs     prometheus.Counter
}

func NewCoordinatorMetrics(reg prometheus.Registerer) *CoordinatorMetrics {
	m := &CoordinatorMetrics{
		ActiveJobs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "coord_active_jobs",
		}),
		SlicingBacklog: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "coord_slicing_backlog",
		}),
		SlicingDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "coord_slicing_duration_seconds",
			Buckets: prometheus.DefBuckets,
		}),
		ManifestDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "coord_manifest_compilation_seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		}),
		BitcountLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "coord_bitcount_latency_ms",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		}),
		PartitionAdoptions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "coord_partition_adoptions_total",
		}),
		DLQDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "coord_dlq_depth",
		}),
		GCOrphanedJobs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "coord_gc_orphaned_jobs_total",
		}),
	}
	reg.MustRegister(m.ActiveJobs, m.SlicingBacklog, m.SlicingDuration,
		m.ManifestDuration, m.BitcountLatency, m.PartitionAdoptions,
		m.DLQDepth, m.GCOrphanedJobs)
	return m
}

// WorkerMetrics contains all Prometheus metrics for the Worker tier.
type WorkerMetrics struct {
	TranscodeDuration  prometheus.Histogram
	FFmpegCrashes      prometheus.Counter
	IdempotencyHits    prometheus.Counter
	S3FallbackTotal    prometheus.Counter
	CircuitBreakerOpen prometheus.Gauge
	DiskFreeBytes      prometheus.Gauge
	NATSInflightTasks  prometheus.Gauge
	GPUUtilization     prometheus.Gauge
}

func NewWorkerMetrics(reg prometheus.Registerer) *WorkerMetrics {
	m := &WorkerMetrics{
		TranscodeDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "worker_transcode_duration_seconds",
			Buckets: []float64{1, 5, 10, 20, 30, 60, 120},
		}),
		FFmpegCrashes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worker_ffmpeg_crashes_total",
		}),
		IdempotencyHits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worker_idempotency_hits_total",
		}),
		S3FallbackTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worker_s3_fallback_total",
		}),
		CircuitBreakerOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worker_circuit_breaker_open",
		}),
		DiskFreeBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worker_disk_free_bytes",
		}),
		NATSInflightTasks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worker_nats_inflight_tasks",
		}),
		GPUUtilization: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worker_gpu_utilization_pct",
		}),
	}
	reg.MustRegister(m.TranscodeDuration, m.FFmpegCrashes, m.IdempotencyHits,
		m.S3FallbackTotal, m.CircuitBreakerOpen, m.DiskFreeBytes,
		m.NATSInflightTasks, m.GPUUtilization)
	return m
}
