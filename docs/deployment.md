# Tessera Production Deployment & Sizing Guide

This guide details CLI usage, configuration environment variables, capacity planning sizing tiers, and multi-region simulations for running Tessera in production.

---

## 1. Local CLI Commands

To boot the component daemons individually:
```bash
# Start Gateway API node
./video-engine server gateway --config config.yaml --region us-east

# Start Coordinator active node (Etcd Ring)
./video-engine server coordinator --config config.yaml --region us-east

# Start Transcoding Worker compute node
./video-engine server worker --config config.yaml --region us-east
```

---

## 2. Environment Variables Configuration

The single CLI binary (`video-engine`) is configured via a `config.yaml` file or environment overrides:

| Environment Variable | Description | Example Value |
| :--- | :--- | :--- |
| `TRANSCODER_REGION` | Current node compute region | `us-east-1` |
| `TRANSCODER_MESSAGE_BUS_PROVIDER` | Messaging queue backend | `nats` (default) or `sqs` |
| `TRANSCODER_REDIS_ADDRS` | Comma-separated list of Redis shard hostnames | `redis-0:6379,redis-1:6379` |
| `TRANSCODER_REDIS_PASSWORD` | Redis cluster authorization password | `my-secure-password` |
| `TRANSCODER_NATS_URLS` | Comma-separated list of NATS server URLs | `nats://nats:4222` |
| `TRANSCODER_ETCD_ENDPOINTS` | Comma-separated list of Etcd API hostnames | `etcd-0:2379,etcd-1:2379` |
| `TRANSCODER_S3_ENDPOINT` | Object storage gateway URL | `s3.us-east-1.amazonaws.com` |
| `TRANSCODER_S3_ACCESS_KEY` | Storage service credentials access key ID | `AKIAIOSFODNN7EXAMPLE` |
| `TRANSCODER_S3_SECRET_KEY` | Storage service credentials secret key | `wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY` |
| `TRANSCODER_S3_BUCKET` | Target bucket name for video media | `tessera-media-prod` |
| `TRANSCODER_JWT_SECRET` | Secret used to sign upload session tokens | `hs256-signing-secret` |
| `TRANSCODER_LISTEN_ADDR` | Port for the Gateway API daemon | `:8080` |

---

## 3. Capacity Sizing Matrix

Deploy according to your target concurrency. Tessera scales horizontally at all tiers.

| Sizing Tier | Active Users | Peak Concurrent Uploads | Recommended VM Instance Sizes | Topology Structure |
| :--- | :--- | :--- | :--- | :--- |
| **Tier 1 (Sandbox)** | 10K | 1–3 | Single `t3.xlarge` (4 vCPU, 16GB RAM) | All daemons + storage (MinIO/Redis/NATS) in Docker Compose on one VM. |
| **Tier 2 (Startup)** | 50K | ~10 | 1x `c6i.xlarge` (App) + 1x `m6i.xlarge` (Infra) | Decouple Redis/NATS/MinIO to a dedicated database VM. |
| **Tier 3 (Growth)** | 200K | ~20 | 2x Gateway, 2x Coord, 4x Worker VMs (c6i.large) | Load balancer for Gateways. Standalone Redis cluster + NATS cluster. |
| **Tier 4 (Decoupled)** | 500K | 10–30 | 3x Gateway, 3x Coord, 10x Worker VMs (c6i.xlarge) | Dedicate VMs for worker fleets. Run database servers on multi-node clusters. |
| **Tier 5 (Enterprise)** | 5M | 300–500 | Kubernetes EKS/GKE (Gateway/Coord/Workers) | Use KEDA for dynamic worker scaling. AWS ElastiCache / Redis Enterprise + S3. |
| **Tier 6 (Global)** | 50M+ | 50,000+ | Kubernetes + CDN Edges + OCI Ampere GPU Workers | Clustered active-active multi-region PoPs. Manifest-only S3 Cross-Region Replication. |

---

## 4. Kubernetes Deployment Snippets

Deploy Gateway, Coordinator, and Worker instances as stateless deployments.

### 1. Gateway Deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tessera-gateway
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: gateway
        image: tessera/engine:latest
        command: ["/usr/local/bin/video-engine", "server", "gateway"]
        ports:
        - containerPort: 8080
        - containerPort: 9090 # Prometheus metrics
        envFrom:
        - configMapRef:
            name: tessera-config
```

### 2. Coordinator Deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tessera-coordinator
spec:
  replicas: 3 # Rings balance partitions automatically across nodes
  template:
    spec:
      containers:
      - name: coordinator
        image: tessera/engine:latest
        command: ["/usr/local/bin/video-engine", "server", "coordinator"]
```

### 3. Worker Deployment (Autoscaled via KEDA)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tessera-worker
spec:
  replicas: 10
  template:
    spec:
      containers:
      - name: worker
        image: tessera/engine:latest
        command: ["/usr/local/bin/video-engine", "server", "worker"]
        resources:
          limits:
            cpu: "2"
            memory: 4Gi
```

---

## 5. Local Multi-Region Simulation

To test multi-region partitioning and regional isolation on a single development machine:
```bash
# Make the simulation script executable and run it
chmod +x scripts/run_simulation.sh
./scripts/run_simulation.sh
```

### 5.1 Multi-Region Port Matrix
The simulation boots two fully isolated stacks (US-East and EU-West) on local Docker networks using port offsets:

| Resource | US-East Port | EU-West Port | Port Offset | S3 Bucket Name |
| :--- | :--- | :--- | :--- | :--- |
| **Gateway API** | `:8080` | `:8090` | +10 | `transcoder-us-east` |
| **MinIO S3 API** | `:9000` | `:9010` | +10 | `transcoder-eu-west` |
| **Redis** | `:6379` | `:6389` | +10 | — |
| **NATS Client** | `:4222` | `:4232` | +10 | — |
| **MinIO Console** | `:9001` | `:9011` | +10 | — |
| **Metrics** | `:9091` | `:9092` | +1 | — |

---

## 6. Testing Regional Isolation

Submit a job to the US-East Gateway API:
```bash
curl -X POST http://127.0.0.1:8080/api/jobs/upload-session \
  -H 'Content-Type: application/json' \
  -d '{"file_size_bytes": 1048576, "file_name": "sample.mp4", "content_type": "video/mp4"}'
```

Monitor execution logs:
- **US-East Workers** will pull, log, and transcode the segments: `tail -f logs/us-east-worker.log`.
- **EU-West Workers** remain completely idle, proving strict regional task isolation: `tail -f logs/eu-west-worker.log`.

---

## 7. Multi-Region Manifest Replication (Cross-Region Replication - CRR)

To minimize WAN egress fees, Tessera separates video segments from playback descriptors:
- **Data Gravity**: Raw segment transport streams (`.ts`) stay local to the region they were processed in.
- **Manifest-only Replication**: The CRR sync loop ([`simulate_crr.go`](../scripts/simulate_crr.go)) periodically scans buckets and replicates only master playlists, variant media playlists, and completion metadata (`master.m3u8`, `manifest.mpd`, `job_completed.json`).
- Viewers in Europe can request the local manifest on port `:8090`, which redirects segment requests to the local EU CDN cache rather than crossing ocean links to fetch heavy video segments.

---

## 8. Common SRE Docker Operations

Use these operations to manage the local docker compose container stack during local development:
```bash
# 1. Scale worker nodes up or down
docker compose scale worker=5

# 2. View real-time logs from all running services
docker compose logs -f

# 3. Stream logs exclusively from the worker containers
docker compose logs -f worker

# 4. Stop the cluster and wipe database volumes (full reset)
docker compose down -v

# 5. Restart a single gateway container after hot reload
docker compose restart gateway
```
