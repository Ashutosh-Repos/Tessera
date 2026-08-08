package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	yamlData := []byte(`
role: "coordinator"
region: "us-east-1"
node_id: "coord-node-1"
message_bus_provider: "nats"

redis:
  addrs:
    - "redis-0:6379"
    - "redis-1:6379"
  password: "secretpassword"
  max_retries: 3
  pool_size: 10

nats:
  urls:
    - "nats://nats-0:4222"

etcd:
  endpoints:
    - "127.0.0.1:2379"

object_store:
  endpoint: "minio:9000"
  bucket: "test-bucket"
  region: "us-east-1"
  access_key: "minioadmin"
  secret_key: "minioadmin"
  use_ssl: false

gateway:
  listen_addr: ":8080"
  jwt_secret: "jwtsecret123"
  admin_api_key: "adminsecret"
  max_upload_size_gb: 50
  rate_limit_per_ip: 100
  rate_limit_per_user: 500
  multiplex_batch_ms: 1000

coordinator:
  partition_count: 1024
  slicing_semaphore: 50
  nats_shard_count: 4
  etcd_lease_ttl_sec: 5
  slicing_lock_ttl_sec: 10
  self_fence_thresh_sec: 3
  takeover_grace_sec: 10
  gc_interval_min: 10
  gc_stale_thresh_hours: 24

worker:
  scratch_dir: "/tmp/scratch"
  min_disk_free_gb: 10
  watchdog_interval_sec: 10
  max_task_duration_min: 5
  max_temp_file_size_gb: 3
  concurrent_tasks: 50
  graceful_drain_sec: 300
  circuit_breaker_window: 5
  circuit_breaker_thresh: 3
  hw_accel: "none"

metrics:
  listen_addr: ":9090"
  path: "/metrics"

tracing:
  endpoint: "otel-collector:4317"
  service_name: "transcoder-service"
  sample_rate: 0.01
`)

	if err := os.WriteFile(configPath, yamlData, 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Role != "coordinator" {
		t.Errorf("cfg.Role = %q, want %q", cfg.Role, "coordinator")
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("cfg.Region = %q, want %q", cfg.Region, "us-east-1")
	}
	if cfg.NodeID != "coord-node-1" {
		t.Errorf("cfg.NodeID = %q, want %q", cfg.NodeID, "coord-node-1")
	}
	if cfg.Worker.NodeID != "coord-node-1" {
		t.Errorf("cfg.Worker.NodeID = %q, want inherited %q", cfg.Worker.NodeID, "coord-node-1")
	}
	if len(cfg.Redis.Addrs) != 2 {
		t.Errorf("len(cfg.Redis.Addrs) = %d, want 2", len(cfg.Redis.Addrs))
	}
	if cfg.Coordinator.PartitionCount != 1024 {
		t.Errorf("cfg.Coordinator.PartitionCount = %d, want 1024", cfg.Coordinator.PartitionCount)
	}
}

func TestLoadConfig_EnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	yamlData := []byte(`
role: "gateway"
region: "us-west"
node_id: "node-orig"
redis:
  addrs: ["127.0.0.1:6379"]
object_store:
  endpoint: "original:9000"
gateway:
  listen_addr: ":8080"
  jwt_secret: "original-secret"
`)
	if err := os.WriteFile(configPath, yamlData, 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Set env overrides
	t.Setenv("TRANSCODER_REDIS_ADDRS", "redis-a:6379,redis-b:6379")
	t.Setenv("TRANSCODER_REDIS_PASSWORD", "env-password")
	t.Setenv("TRANSCODER_NATS_URLS", "nats://nats-env:4222")
	t.Setenv("TRANSCODER_ETCD_ENDPOINTS", "10.0.0.1:2379")
	t.Setenv("TRANSCODER_S3_ENDPOINT", "minio-env:9000")
	t.Setenv("TRANSCODER_S3_ACCESS_KEY", "env-access")
	t.Setenv("TRANSCODER_S3_SECRET_KEY", "env-secret")
	t.Setenv("TRANSCODER_S3_BUCKET", "env-bucket")
	t.Setenv("TRANSCODER_JWT_SECRET", "env-jwt-secret")
	t.Setenv("TRANSCODER_REGION", "eu-central-1")
	t.Setenv("TRANSCODER_LISTEN_ADDR", ":9999")
	t.Setenv("TRANSCODER_MESSAGE_BUS_PROVIDER", "sqs")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if len(cfg.Redis.Addrs) != 2 || cfg.Redis.Addrs[0] != "redis-a:6379" {
		t.Errorf("cfg.Redis.Addrs = %v, want env override", cfg.Redis.Addrs)
	}
	if cfg.Redis.Password != "env-password" {
		t.Errorf("cfg.Redis.Password = %q, want %q", cfg.Redis.Password, "env-password")
	}
	if len(cfg.NATS.URLs) != 1 || cfg.NATS.URLs[0] != "nats://nats-env:4222" {
		t.Errorf("cfg.NATS.URLs = %v, want env override", cfg.NATS.URLs)
	}
	if len(cfg.Etcd.Endpoints) != 1 || cfg.Etcd.Endpoints[0] != "10.0.0.1:2379" {
		t.Errorf("cfg.Etcd.Endpoints = %v, want env override", cfg.Etcd.Endpoints)
	}
	if cfg.ObjectStore.Endpoint != "minio-env:9000" {
		t.Errorf("cfg.ObjectStore.Endpoint = %q, want %q", cfg.ObjectStore.Endpoint, "minio-env:9000")
	}
	if cfg.ObjectStore.AccessKey != "env-access" || cfg.ObjectStore.SecretKey != "env-secret" || cfg.ObjectStore.Bucket != "env-bucket" {
		t.Errorf("cfg.ObjectStore credentials = %v, want env overrides", cfg.ObjectStore)
	}
	if cfg.Gateway.JWTSecret != "env-jwt-secret" {
		t.Errorf("cfg.Gateway.JWTSecret = %q, want %q", cfg.Gateway.JWTSecret, "env-jwt-secret")
	}
	if cfg.Region != "eu-central-1" {
		t.Errorf("cfg.Region = %q, want %q", cfg.Region, "eu-central-1")
	}
	if cfg.Gateway.ListenAddr != ":9999" {
		t.Errorf("cfg.Gateway.ListenAddr = %q, want %q", cfg.Gateway.ListenAddr, ":9999")
	}
	if cfg.MessageBusProvider != "sqs" {
		t.Errorf("cfg.MessageBusProvider = %q, want %q", cfg.MessageBusProvider, "sqs")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Errorf("LoadConfig(nonexistent) = nil error, want error")
	}
}
