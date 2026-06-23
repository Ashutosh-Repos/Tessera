# Infrastructure Specification: Cloud-Agnostic Kubernetes & Bare-Metal

This document defines the physical hardware and Kubernetes orchestration requirements for deploying the Distributed Transcoding Engine at the **50 Million User** scale. 

Because the architecture relies on a **Shared-Nothing (SN)** model and standard open-source protocols, it avoids vendor lock-in and can be deployed entirely on bare-metal servers or cloud-agnostic Kubernetes clusters (GKE, AKS, standard k8s).

---

## 1. Hardware Fleet Sizing (50M User Peak)

The fleet is strictly tiered to prevent resource contention. Each tier scales independently based on distinct metrics.

### 1.1 Gateway Tier (Ingress & WebSockets)
* **Role**: Terminates TLS, validates JWTs, generates MinIO upload URLs, and multiplexes Redis progress streams to WebSockets.
* **Nodes**: ~100 nodes at peak
* **Hardware Profile**: 4 vCPU, 16 GB RAM. High network I/O. (Equivalent: AWS `c6gn.xlarge` / GCP `c2-standard-4`). No GPU required.
* **Kubernetes Primitive**: `Deployment` fronted by an Ingress Controller (NGINX/Traefik).

### 1.2 Coordinator Tier (Control Plane)
* **Role**: GOP-aligned stream slicing, NATS task publishing, state reconciliation, and manifest compiling.
* **Nodes**: ~50 nodes at peak
* **Hardware Profile**: 8 vCPU, 64 GB RAM. (Equivalent: AWS `r6g.2xlarge` / GCP `n2-highmem-8`). High RAM is required to hold the 1024 virtual partition maps. No GPU required.
* **Kubernetes Primitive**: `StatefulSet` or `Deployment` with strict `podAntiAffinity` to ensure coordinators are spread across physical racks for fault tolerance.

### 1.3 Worker Tier (Compute Plane)
* **Role**: Pulls NATS tasks, executes FFmpeg hardware transcoding, pushes to MinIO.
* **Nodes**: ~10,000 nodes at peak
* **Hardware Profile**: 4 vCPU, 16 GB RAM, **1× NVIDIA T4 or A10G GPU**, 500GB local NVMe SSD.
* **Kubernetes Primitive**: `Deployment` using `nodeSelector` or `tolerations` to schedule exclusively on GPU-equipped nodes.

---

## 2. Stateful Backing Services (Helm Deployments)

We self-host the state tier inside the Kubernetes cluster using enterprise-grade Bitnami Helm charts.

### 2.1 Object Storage: MinIO Cluster
**Replaces AWS S3.** MinIO provides distributed, high-performance S3-compatible object storage.
* **Architecture**: Distributed MinIO cluster spanning dozens of storage-heavy bare-metal nodes.
* **Eventing Bridge**: **Replaces AWS SQS.** MinIO natively integrates with NATS. We configure MinIO to publish `s3:ObjectCreated:Put` events directly to the NATS JetStream cluster, eliminating the need for a polling bridge.
* **Networking**: MinIO nodes require dual-port 100Gbps NICs to absorb the 1 TB/s peak video ingestion and delivery bandwidth.

### 2.2 In-Memory State: Redis Cluster
* **Role**: Job progress bitmaps, WebSocket streams, and idempotency caching.
* **Architecture**: 6 Nodes (3 Masters, 3 Replicas) deployed via Bitnami Redis Cluster Helm chart.
* **Storage**: 32 GB RAM per node. Persistent storage is not strictly required since S3/MinIO is the durable source of truth, but AOF can be enabled for faster recovery.

### 2.3 Consensus: etcd
* **Role**: Coordinator registration and slicing mutual-exclusion locks.
* **Architecture**: 5 Nodes deployed via Bitnami etcd Helm chart.
* **Storage Requirement (CRITICAL)**: etcd requires `<10ms` fsync disk latency to prevent Raft leader elections from flapping. Nodes MUST be backed by local NVMe drives mounted as `PersistentVolumeClaims` (PVCs). Network-attached block storage (like standard EBS/PD) is strongly discouraged for this tier.

### 2.4 Event Bus: NATS JetStream
* **Role**: Task distribution, Dead Letter Queues, and task claiming (`AckWait`).
* **Architecture**: 5 Nodes deployed via the official NATS Helm chart.
* **Storage Requirement**: High-IOPS PersistentVolumes for the Write-Ahead Log (WAL) to ensure tasks are durably persisted before coordinators receive ACKs.

---

## 3. Storage & Local Disk Fencing

The architecture requires explicit disk management on the Worker tier to prevent local SSD saturation during massive 4K file downloads.

* **Scratch Space**: Worker pods require a `hostPath` or `emptyDir` mount pointing to a physical local NVMe drive (`/tmp/scratch`).
* **Fencing**: Before accepting a NATS task, the worker queries `syscall.Statfs("/tmp/scratch")`. If the node has `<10GB` free space, the task is rejected (triggering NATS redelivery to a healthier node) and the worker isolates itself until its background garbage collector cleans the drive.

---

## 4. Network Topology & Security

### 4.1 Internal Service Mesh
* Internal traffic between Gateway, Coordinator, and Worker tiers is secured using a Service Mesh (e.g., **Istio** or **Linkerd**) enforcing strict mTLS. 
* This satisfies zero-trust security without modifying the Go application code.

### 4.2 Traffic Separation (Control vs. Storage)
At 50M users, the network fabric will process over **50 Tbps** of aggregate bandwidth.
* **Control Plane Network**: A standard 10/25Gbps VLAN handles NATS messages, Redis pipelines, etcd heartbeats, and WebSocket JSON streams.
* **Storage Data Plane Network**: A dedicated 100Gbps RDMA/RoCE backend fabric connects the Worker nodes directly to the MinIO cluster. Video segment downloads (`ListObjects` / `GetObjects`) and uploads (`PutObject`) must never cross the Control Plane Network to prevent saturating NATS or etcd heartbeats.

### 4.3 External CDN
* External video delivery is fronted by a global CDN (Cloudflare, Fastly, or Akamai).
* The CDN points directly to the MinIO Ingress. MinIO acts as the Origin Shield. 
* Once the final HLS `.m3u8` or DASH `.mpd` manifests are generated, the Gateway triggers a CDN pre-fetch.
