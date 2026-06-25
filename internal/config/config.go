package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the unified configuration for all three tiers.
// Each tier reads only the sections relevant to its role.
type Config struct {
	Role   string `yaml:"role"`    // gateway | coordinator | worker
	Region string `yaml:"region"`  // e.g. "us-east-1"
	NodeID string `yaml:"node_id"` // unique per-node identifier

	// Infrastructure Endpoints
	Redis       RedisConfig       `yaml:"redis"`
	NATS        NATSConfig        `yaml:"nats"`
	Etcd        EtcdConfig        `yaml:"etcd"`
	ObjectStore ObjectStoreConfig `yaml:"object_store"` // MinIO / S3-compatible

	// Tier-Specific
	Gateway     GatewayConfig     `yaml:"gateway"`
	Coordinator CoordinatorConfig `yaml:"coordinator"`
	Worker      WorkerConfig      `yaml:"worker"`

	// Observability
	Metrics MetricsConfig `yaml:"metrics"`
	Tracing TracingConfig `yaml:"tracing"`
}

type RedisConfig struct {
	Addrs      []string `yaml:"addrs"` // e.g. ["redis-0:6379","redis-1:6379","redis-2:6379"]
	Password   string   `yaml:"password"`
	MaxRetries int      `yaml:"max_retries"`
	PoolSize   int      `yaml:"pool_size"` // per shard
}

type NATSConfig struct {
	URLs    []string `yaml:"urls"`     // e.g. ["nats://nats-0:4222"]
	TLSCert string   `yaml:"tls_cert"` // mTLS client cert path
	TLSKey  string   `yaml:"tls_key"`
	TLSCA   string   `yaml:"tls_ca"`
}

type EtcdConfig struct {
	Endpoints []string `yaml:"endpoints"`
	TLSCert   string   `yaml:"tls_cert"`
	TLSKey    string   `yaml:"tls_key"`
	TLSCA     string   `yaml:"tls_ca"`
}

type ObjectStoreConfig struct {
	Endpoint  string `yaml:"endpoint"` // e.g. "minio.internal:9000"
	Bucket    string `yaml:"bucket"`
	Region    string `yaml:"region"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	UseSSL    bool   `yaml:"use_ssl"`
}

type GatewayConfig struct {
	ListenAddr       string `yaml:"listen_addr"`         // ":8080"
	JWTSecret        string `yaml:"jwt_secret"`
	MaxUploadSizeGB  int    `yaml:"max_upload_size_gb"`  // 50
	RateLimitPerIP   int    `yaml:"rate_limit_per_ip"`   // 100/min
	RateLimitPerUser int    `yaml:"rate_limit_per_user"` // 500/day
	MultiplexBatchMs int    `yaml:"multiplex_batch_ms"`  // 1000 (XREAD BLOCK timeout)
}

type CoordinatorConfig struct {
	PartitionCount     int `yaml:"partition_count"`       // 1024
	SlicingSemaphore   int `yaml:"slicing_semaphore"`     // 50
	NATSShardCount     int `yaml:"nats_shard_count"`      // 4
	EtcdLeaseTTLSec    int `yaml:"etcd_lease_ttl_sec"`    // 5
	SlicingLockTTLSec  int `yaml:"slicing_lock_ttl_sec"`  // 10
	SelfFenceThreshSec int `yaml:"self_fence_thresh_sec"` // 3
	TakeoverGraceSec   int `yaml:"takeover_grace_sec"`    // 10
	GCIntervalMin      int `yaml:"gc_interval_min"`       // 10
	GCStaleThreshHours int `yaml:"gc_stale_thresh_hours"` // 24
}

type WorkerConfig struct {
	NodeID               string `yaml:"node_id"`                // inherited from global Config.NodeID at init
	ScratchDir           string `yaml:"scratch_dir"`            // "/tmp/scratch"
	MinDiskFreeGB        int    `yaml:"min_disk_free_gb"`       // 10
	WatchdogIntervalSec  int    `yaml:"watchdog_interval_sec"`  // 10
	MaxTaskDurationMin   int    `yaml:"max_task_duration_min"`  // 5
	MaxTempFileSizeGB    int    `yaml:"max_temp_file_size_gb"`  // 3
	ConcurrentTasks      int    `yaml:"concurrent_tasks"`       // 50 (per worker node)
	GracefulDrainSec     int    `yaml:"graceful_drain_sec"`     // 300 (5 minutes)
	CircuitBreakerWindow int    `yaml:"circuit_breaker_window"` // 5 seconds
	CircuitBreakerThresh int    `yaml:"circuit_breaker_thresh"` // 3 failures
	HWAccel              string `yaml:"hw_accel"`               // "nvenc" | "vaapi" | "videotoolbox" | "none"
}

type MetricsConfig struct {
	ListenAddr string `yaml:"listen_addr"` // ":9090"
	Path       string `yaml:"path"`        // "/metrics"
}

type TracingConfig struct {
	Endpoint    string  `yaml:"endpoint"`     // "otel-collector:4317"
	ServiceName string  `yaml:"service_name"` // "transcoder-gateway"
	SampleRate  float64 `yaml:"sample_rate"`  // 0.01 (1%)
}

// LoadConfig reads a YAML config file and parses it into the Config struct.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("failed to read config file: %w", err)
	}

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Environment variable overrides for production configurations
	if addrs := os.Getenv("TRANSCODER_REDIS_ADDRS"); addrs != "" {
		cfg.Redis.Addrs = strings.Split(addrs, ",")
	}
	if password := os.Getenv("TRANSCODER_REDIS_PASSWORD"); password != "" {
		cfg.Redis.Password = password
	}
	if urls := os.Getenv("TRANSCODER_NATS_URLS"); urls != "" {
		cfg.NATS.URLs = strings.Split(urls, ",")
	}
	if eps := os.Getenv("TRANSCODER_ETCD_ENDPOINTS"); eps != "" {
		cfg.Etcd.Endpoints = strings.Split(eps, ",")
	}
	if s3Ep := os.Getenv("TRANSCODER_S3_ENDPOINT"); s3Ep != "" {
		cfg.ObjectStore.Endpoint = s3Ep
	}
	if s3Key := os.Getenv("TRANSCODER_S3_ACCESS_KEY"); s3Key != "" {
		cfg.ObjectStore.AccessKey = s3Key
	}
	if s3Sec := os.Getenv("TRANSCODER_S3_SECRET_KEY"); s3Sec != "" {
		cfg.ObjectStore.SecretKey = s3Sec
	}
	if s3Bkt := os.Getenv("TRANSCODER_S3_BUCKET"); s3Bkt != "" {
		cfg.ObjectStore.Bucket = s3Bkt
	}
	if jwtSec := os.Getenv("TRANSCODER_JWT_SECRET"); jwtSec != "" {
		cfg.Gateway.JWTSecret = jwtSec
	}
	if reg := os.Getenv("TRANSCODER_REGION"); reg != "" {
		cfg.Region = reg
	}
	if addr := os.Getenv("TRANSCODER_LISTEN_ADDR"); addr != "" {
		cfg.Gateway.ListenAddr = addr
	}

	// Propagate NodeID to worker config to avoid LLD B-7 compile errors
	cfg.Worker.NodeID = cfg.NodeID

	return cfg, nil
}
