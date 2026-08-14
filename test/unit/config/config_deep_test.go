package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/distributed-transcoder/internal/config"
)

func Test_Config_LoadConfig_ValidYAML_ParsesAllFields(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	yamlContent := `
role: "gateway"
region: "us-west-2"
node_id: "node-gateway-01"
message_bus_provider: "nats"

redis:
  addrs:
    - "redis-0:6379"
    - "redis-1:6379"
  password: "redis-password"
  max_retries: 5
  pool_size: 100

nats:
  urls:
    - "nats://nats-cluster:4222"

gateway:
  listen_addr: ":9000"
  jwt_secret: "super-secret"
  max_upload_size_gb: 40

coordinator:
  partition_count: 512
  slicing_semaphore: 20

worker:
  concurrent_tasks: 64
  scratch_dir: "/data/scratch"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Role != "gateway" || cfg.Region != "us-west-2" || cfg.NodeID != "node-gateway-01" {
		t.Errorf("global fields mismatch: %+v", cfg)
	}
	if len(cfg.Redis.Addrs) != 2 || cfg.Redis.Password != "redis-password" {
		t.Errorf("redis config mismatch: %+v", cfg.Redis)
	}
	if cfg.Gateway.ListenAddr != ":9000" || cfg.Gateway.JWTSecret != "super-secret" || cfg.Gateway.MaxUploadSizeGB != 40 {
		t.Errorf("gateway config mismatch: %+v", cfg.Gateway)
	}
	if cfg.Coordinator.PartitionCount != 512 || cfg.Coordinator.SlicingSemaphore != 20 {
		t.Errorf("coordinator config mismatch: %+v", cfg.Coordinator)
	}
	if cfg.Worker.ConcurrentTasks != 64 || cfg.Worker.ScratchDir != "/data/scratch" {
		t.Errorf("worker config mismatch: %+v", cfg.Worker)
	}
	if cfg.Worker.NodeID != "node-gateway-01" {
		t.Errorf("worker NodeID not propagated from global config: %s", cfg.Worker.NodeID)
	}
}

func Test_Config_LoadConfig_EnvironmentVariableOverrides(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	yamlContent := `
region: "us-east-1"
gateway:
  jwt_secret: "original-secret"
`
	_ = os.WriteFile(configPath, []byte(yamlContent), 0644)

	t.Setenv("TRANSCODER_REGION", "eu-central-1")
	t.Setenv("TRANSCODER_JWT_SECRET", "env-secret-key")
	t.Setenv("TRANSCODER_REDIS_ADDRS", "r1:6379,r2:6379")
	t.Setenv("TRANSCODER_MESSAGE_BUS_PROVIDER", "sqs")

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Region != "eu-central-1" {
		t.Errorf("expected Region override eu-central-1, got %s", cfg.Region)
	}
	if cfg.Gateway.JWTSecret != "env-secret-key" {
		t.Errorf("expected JWTSecret override env-secret-key, got %s", cfg.Gateway.JWTSecret)
	}
	if len(cfg.Redis.Addrs) != 2 || cfg.Redis.Addrs[0] != "r1:6379" {
		t.Errorf("expected Redis addrs override, got %+v", cfg.Redis.Addrs)
	}
	if cfg.MessageBusProvider != "sqs" {
		t.Errorf("expected MessageBusProvider override sqs, got %s", cfg.MessageBusProvider)
	}
}

func Test_Config_LoadConfig_DefaultFallbacks(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "empty_config.yaml")
	_ = os.WriteFile(configPath, []byte("{}"), 0644)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Gateway defaults
	if cfg.Gateway.RateLimitPerIP != 100 || cfg.Gateway.RateLimitPerUser != 500 || cfg.Gateway.MultiplexBatchMs != 1000 {
		t.Errorf("gateway defaults not applied: %+v", cfg.Gateway)
	}

	// Coordinator defaults
	if cfg.Coordinator.PartitionCount != 1024 || cfg.Coordinator.SlicingSemaphore != 50 || cfg.Coordinator.NATSShardCount != 4 {
		t.Errorf("coordinator defaults not applied: %+v", cfg.Coordinator)
	}

	// Worker defaults
	if cfg.Worker.ConcurrentTasks != 50 || cfg.Worker.MinDiskFreeGB != 10 || cfg.Worker.GracefulDrainSec != 300 {
		t.Errorf("worker defaults not applied: %+v", cfg.Worker)
	}
}

func Test_Config_LoadConfig_MissingFile_ReturnsError(t *testing.T) {
	_, err := config.LoadConfig("/path/to/non/existent/config.yaml")
	if err == nil {
		t.Errorf("expected error on missing config file, got nil")
	}
}
