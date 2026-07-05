# V2 Idea 1: Oracle OCI (Kafka) and GCP Pub/Sub Message Bus Drivers

## The Goal
To make the engine truly cloud-agnostic across all major providers, V2 will add native support for **Google Cloud Pub/Sub** and **Oracle Cloud Infrastructure (OCI) Streaming**.

OCI Streaming is 100% Kafka-compatible. Therefore, by implementing a Kafka driver, the VOD engine automatically gains support for Oracle Cloud, Confluent Cloud, Amazon MSK, and self-hosted Apache Kafka.

## Proposed Changes for V2

### 1. Update Dependencies
*   Add `cloud.google.com/go/pubsub` to `go.mod` for GCP.
*   Add `github.com/segmentio/kafka-go` to `go.mod` for OCI/Kafka (this library is pure Go and doesn't require complex C-bindings like `confluent-kafka-go`, keeping the Dockerfile small).

### 2. Configuration Updates
Update `internal/config/config.go` to add configuration structs for the new drivers:
```go
type GCPPubSubConfig struct {
    ProjectID string `yaml:"project_id"`
    TopicName string `yaml:"topic_name"`
}
type KafkaConfig struct {
    Brokers  []string `yaml:"brokers"`
    Username string   `yaml:"username"` // Needed for OCI Streaming SASL
    Password string   `yaml:"password"`
}
```

### 3. Implement the Drivers
*   **Create `internal/infra/gcp_pubsub.go`**:
    Implement the 8 required functions of the `MessageBus` interface (PublishTaskAsync, PullTasks, Subscribe, etc.) using the GCP Go SDK.
*   **Create `internal/infra/kafka_bus.go`**:
    Implement the same interface using the `kafka-go` library. Configure SASL/PLAIN authentication so it can connect directly to Oracle OCI Streaming securely.

### 4. Wire the Drivers to the Engine
Update the `initInfra` switch statement in `cmd/transcoder/main.go`:
```go
if cfg.MessageBusProvider == "gcp" {
    messageBus, err = infra.NewGCPPubSubBus(cfg.GCPPubSub)
} else if cfg.MessageBusProvider == "kafka" || cfg.MessageBusProvider == "oci" {
    messageBus, err = infra.NewKafkaBus(cfg.Kafka)
}
```

## Why this is a V2 Feature
Adding these SDKs significantly increases the binary size and dependency footprint of the Go project. By pushing this to V2, we keep V1 extremely lightweight (only containing AWS and NATS drivers) while documenting the exact roadmap for full universal cloud support.
