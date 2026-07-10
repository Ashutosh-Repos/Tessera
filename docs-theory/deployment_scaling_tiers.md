# Capacity Planning & Deployment Scaling Reference Manual

This manual provides concrete, quantitative hardware specifications, VM instance sizes, cluster structures, and database configurations for deploying the **Tessera Distributed Transcoder System** across six sequential load levels (from developer sandbox to YouTube-scale global networks).

---

## 📊 Sizing and Load Matrix

| Scale Tier | Target User Base | Avg. Daily Uploads | Concurrent Uploads (Peak) | Target Transcode Throughput |
| :--- | :--- | :--- | :--- | :--- |
| **Tier 1 (Developer)** | 10K Users | 10–50 videos / day | 1–3 concurrent | ~2 hours of video / hour |
| **Tier 2 (Startup)** | 50K Users | 100–150 videos / day | ~10 concurrent | ~6 hours of video / hour |
| **Tier 3 (Growth)** | 200K Users | 300–400 videos / day | ~20 concurrent | ~15 hours of video / hour |
| **Tier 4 (Decoupled)** | 500K Users | 500–1K videos / day | 10–30 concurrent | ~25 hours of video / hour |
| **Tier 5 (Enterprise)** | 5M Users | ~10K videos / hour | 300–500 concurrent | ~500 hours of video / hour |
| **Tier 6 (Global/YouTube)**| 50M+ Users | ~500 hours / minute | 50,000+ concurrent | 30,000+ hours of video / hour |

---

## ⚙️ Architectural Topologies & VM Specifications

### 🟢 Tier 1: Small Sandbox / Developer (10K Users)
*Runs on a single virtual machine using container virtualization.*

```
 ┌──────────────────────────────────────────────┐
 │ Single Host VM (t3.xlarge equivalent)        │
 │                                              │
 │ [Gateway API] ──► [Coordinator] ──► [Worker]  │
 │      │                 │                 │   │
 │      ▼                 ▼                 ▼   │
 │   [Redis] ────────── [NATS] ───────► [MinIO] │
 └──────────────────────────────────────────────┘
```

#### Hardware & VM Configuration
* **System Host VM**:
  * **VM Instance Type**: AWS `t3.xlarge` (or generic 4 vCPUs, 16GB RAM VM)
  * **Disks**: 100GB Boot SSD (gp3) + 2TB attached SSD Volume (gp3) for media storage
  * **Network**: Up to 5 Gbps burst link
  * **Role**: Gateway, Coordinator, Worker, Redis, NATS, and MinIO running locally in containers.

#### Configuration (`config.yaml`)
```yaml
coordinator:
  partition_count: 8
  nats_shard_count: 1
worker:
  concurrency: 2
  encoder_type: "cpu"
  segment_length_sec: 5
```

---

### 🟢 Tier 2: Mid-Market Startup (50K Users)
*Splits application processes from intermediate data stores to protect system stability.*

```
   ┌───────────────────────────┐         ┌───────────────────────────┐
   │ VM 1: Application Nodes   │         │ VM 2: Database / Storage  │
   │                           │         │                           │
   │ [Gateway]  [Coordinator]  │         │ [Redis]   [NATS Stream]   │
   │      │           │        │         │             │             │
   │      ▼           ▼        │         │             ▼             │
   │ ┌───────────────────────┐ │         │        ┌──────────┐       │
   │ │  Worker (CPU rendering)│ ├─────────┼───────►│  MinIO   │       │
   │ └───────────────────────┘ │         │        └──────────┘       │
   └───────────────────────────┘         └───────────────────────────┘
```

#### Hardware & VM Configuration
* **VM 1: Application Host (Gateway, Coordinator, Worker)**:
  * **VM Instance Type**: AWS `c6i.xlarge` (4 vCPUs, 8GB RAM)
  * **Disks**: 80GB gp3 (for transient ffmpeg chunk temp files)
  * **Network**: Up to 12.5 Gbps
  * **Worker Concurrency**: 3 parallel CPU transcode tasks
* **VM 2: Infrastructure Host (Redis, NATS, Standalone MinIO)**:
  * **VM Instance Type**: AWS `m6i.xlarge` (4 vCPUs, 16GB RAM)
  * **Disks**: 100GB gp3 + 5TB attached SSD Block Storage (gp3)
  * **Network**: Up to 12.5 Gbps

#### Configuration (`config.yaml`)
```yaml
coordinator:
  partition_count: 32
  nats_shard_count: 2
worker:
  concurrency: 3
  encoder_type: "cpu"
  segment_length_sec: 5
```

---

### 🟡 Tier 3: Growth Platform (200K Users)
*Fully decoupled node clusters with basic master-replica database layouts.*

```
                       ┌────────────────┐
                       │  Load Balancer │
                       └───────+────────┘
             ┌─────────────────┴─────────────────┐
             ▼                                   ▼
     ┌───────────────┐                   ┌───────────────┐
     │ Gateway VM 1  │                   │ Gateway VM 2  │
     └───────┬───────┘                   └───────┬───────┘
             └─────────────────┬─────────────────┘
                               ▼
     [Coordinators] ──► [Workers Pool] ──► [Shared Infrastructure]
```

#### Hardware & VM Configuration
* **Ingest Gateways** (2 VMs):
  * **VM Instance Type**: AWS `t3.medium` (2 vCPUs, 4GB RAM)
  * **Disks**: 50GB gp3
  * **Network**: Up to 5 Gbps
* **Coordinators** (2 VMs - Active/Passive via etcd):
  * **VM Instance Type**: AWS `t3.medium` (2 vCPUs, 4GB RAM)
  * **Disks**: 50GB gp3
  * **Network**: Up to 5 Gbps
* **Workers** (3 VMs):
  * **VM Instance Type**: AWS `c6i.xlarge` (4 vCPUs, 8GB RAM)
  * **Disks**: 120GB gp3
  * **Network**: Up to 12.5 Gbps
  * **Worker Concurrency**: 2 parallel tasks per VM (6 concurrent total)
* **Redis Store** (2 VMs):
  * **VM Instance Type**: AWS `t3.medium` (2 vCPUs, 4GB RAM) (1 Primary, 1 Read Replica)
  * **Network**: Up to 5 Gbps
* **NATS & etcd Broker** (1 VM):
  * **VM Instance Type**: AWS `c6i.large` (2 vCPUs, 4GB RAM)
  * **Network**: Up to 12.5 Gbps
* **Object Storage (MinIO)** (2 VMs):
  * **VM Instance Type**: AWS `c6i.xlarge` (4 vCPUs, 8GB RAM)
  * **Disks**: 10TB attached SSD gp3 per VM (20TB total raw capacity)
  * **Network**: Up to 12.5 Gbps

#### Configuration (`config.yaml`)
```yaml
coordinator:
  partition_count: 64
  nats_shard_count: 2
worker:
  concurrency: 2
  encoder_type: "cpu"
  segment_length_sec: 5
```

---

### 🟡 Tier 4: Decoupled Production (500K Users)
*Production clusters running database pools, distributed storage arrays, and high-performance workers.*

#### Hardware & VM Configuration
* **Ingest Gateways** (2 VMs behind ALB):
  * **VM Instance Type**: AWS `c6i.large` (2 vCPUs, 4GB RAM)
  * **Disks**: 50GB gp3
  * **Network**: Up to 12.5 Gbps
* **Coordinators** (3 VMs - Active Ring):
  * **VM Instance Type**: AWS `t3.medium` (2 vCPUs, 4GB RAM)
  * **Disks**: 40GB gp3
  * **Network**: Up to 5 Gbps
* **Workers** (4 VMs):
  * **VM Instance Type**: AWS `c6i.2xlarge` (8 vCPUs, 16GB RAM)
  * **Disks**: 200GB gp3
  * **Network**: Up to 12.5 Gbps
  * **Worker Concurrency**: 4 parallel tasks per VM (16 concurrent total)
* **Redis Sentinel** (3 VMs):
  * **VM Instance Type**: AWS `t3.medium` (2 vCPUs, 4GB RAM)
  * **Network**: Up to 5 Gbps
* **NATS Broker & etcd** (3 VMs):
  * **VM Instance Type**: AWS `c6i.large` (2 vCPUs, 4GB RAM)
  * **Network**: Up to 12.5 Gbps
* **Distributed MinIO Storage** (4 VMs):
  * **VM Instance Type**: AWS `i3en.large` (2 vCPUs, 16GB RAM)
  * **Disks**: 1 x 1.25TB local NVMe SSD per VM (5TB total fast-write capacity)
  * **Network**: Up to 25 Gbps

#### Configuration (`config.yaml`)
```yaml
coordinator:
  partition_count: 128
  nats_shard_count: 4
worker:
  concurrency: 4
  encoder_type: "cpu"
  segment_length_sec: 5
```

---

### 🟠 Tier 5: High Scale / Enterprise (5M Users)
*GPU-accelerated workers and independent sharded database arrays to scale through massive throughput peak loads.*

#### Hardware & VM Configuration
* **Ingest Gateways** (10–12 nodes on Kubernetes EKS/GKE cluster):
  * **VM Instance Type**: AWS `c6i.xlarge` (4 vCPUs, 8GB RAM)
  * **Network**: Up to 12.5 Gbps
* **Coordinators** (3 VMs):
  * **VM Instance Type**: AWS `c6i.xlarge` (4 vCPUs, 8GB RAM)
  * **Network**: Up to 12.5 Gbps
* **Workers (GPU Accelerated)** (16 VMs):
  * **VM Instance Type**: AWS `g4dn.xlarge` (4 vCPUs, 16GB RAM, 1 NVIDIA T4 GPU)
  * **Disks**: 1 x 125GB NVMe SSD (Local temp processing cache)
  * **Network**: Up to 25 Gbps
  * **Worker Concurrency**: 8 concurrent transcode tasks per GPU VM (128 concurrent total)
* **Redis Cluster** (6 VMs):
  * **VM Instance Type**: AWS `r6i.xlarge` (4 vCPUs, 32GB RAM)
  * **Network**: Up to 12.5 Gbps
* **NATS Brokers** (3 VMs):
  * **VM Instance Type**: AWS `c6i.2xlarge` (8 vCPUs, 16GB RAM)
  * **Network**: Up to 12.5 Gbps
* **etcd Cluster** (3 VMs):
  * **VM Instance Type**: AWS `c6i.xlarge` (4 vCPUs, 8GB RAM)
  * **Disks**: 50GB gp3 SSD-optimized (high write IOPS for leases)
  * **Network**: Up to 12.5 Gbps
* **Object Storage (Distributed S3 / Ceph)** (8 VMs):
  * **VM Instance Type**: AWS `i3en.2xlarge` (8 vCPUs, 64GB RAM)
  * **Disks**: 2 x 2.5TB local NVMe SSDs per node (40TB raw fast-access storage)
  * **Network**: Up to 25 Gbps

#### Configuration (`config.yaml`)
```yaml
coordinator:
  partition_count: 512
  nats_shard_count: 16
worker:
  concurrency: 8
  encoder_type: "gpu"            # nvenc hardware acceleration
  segment_length_sec: 5
```

---

### 🔴 Tier 6: Global / YouTube Scale (50M+ Users)
*Multi-Region active PoPs using data gravity topology. Specs listed below are **per active region** (e.g., US-East).*

```
                             [ Geo-DNS Routing ]
                                     │
                 ┌───────────────────┴───────────────────┐
                 ▼                                       ▼
     ┌───────────────────────┐               ┌───────────────────────┐
     │ US-East Region        │               │ EU-West Region        │
     │                       │               │                       │
     │ [30 Ingest Gateways]  │               │ [30 Ingest Gateways]  │
     │ [10 Coordinators]     │               │ [10 Coordinators]     │
     │ [120 GPU Workers]     │               │ [120 GPU Workers]     │
     │ [Redis Cluster (256G)]│               │ [Redis Cluster (256G)]│
     │ [NATS JetStream (32C)]│               │ [NATS JetStream (32C)]│
     │ [Ceph S3 Array (50PB)]│               │ [Ceph S3 Array (50PB)]│
     └───────────┬───────────┘               └───────────┬───────────┘
                 │                                       │
                 └───────────────► [ S3 CRR ] ◄──────────┘
                       (Manifests & Playlists Sync)
```

#### Hardware & VM Configuration (Per Region)
* **Ingest Gateways** (30 VMs behind Geo-Load Balancers):
  * **VM Instance Type**: AWS `c6i.4xlarge` (16 vCPUs, 32GB RAM)
  * **Network**: 25 Gbps ports
* **Coordinators** (10 VMs):
  * **VM Instance Type**: AWS `c6i.4xlarge` (16 vCPUs, 32GB RAM)
  * **Network**: 25 Gbps ports
* **Workers (Dedicated GPU/ASIC)** (120 VMs):
  * **VM Instance Type**: AWS `g5.4xlarge` (16 vCPUs, 64GB RAM, 1 NVIDIA A10G GPU)
  * **Disks**: 1 x 600GB local NVMe SSD
  * **Network**: 25 Gbps ports
  * **Worker Concurrency**: 32 concurrent transcode threads per VM (3,840 concurrent per region)
* **Redis Cluster** (12 VMs):
  * **VM Instance Type**: AWS `r6i.4xlarge` (16 vCPUs, 128GB RAM)
  * **Network**: 25 Gbps ports
* **NATS JetStream Nodes** (5 VMs):
  * **VM Instance Type**: AWS `c6i.8xlarge` (32 vCPUs, 64GB RAM)
  * **Network**: 25 Gbps ports
* **etcd Consensus Nodes** (5 VMs):
  * **VM Instance Type**: AWS `c6i.4xlarge` (16 vCPUs, 32GB RAM)
  * **Disks**: 100GB Local NVMe (High IOPS / sub-millisecond sync latency)
  * **Network**: 25 Gbps ports
* **Object Storage (Distributed Ceph / Storage Array)** (32 VMs):
  * **VM Instance Type**: AWS `i3en.6xlarge` (24 vCPUs, 192GB RAM)
  * **Disks**: 4 x 7.5TB NVMe SSDs per node (240TB fast storage per VM, ~7.6PB raw total capacity)
  * **Network**: 25 Gbps ports

#### Configuration (`config.yaml`)
```yaml
coordinator:
  partition_count: 1024
  nats_shard_count: 64
worker:
  concurrency: 32
  encoder_type: "gpu"            # Dedicated GPU transcode pipeline
  segment_length_sec: 5
```
