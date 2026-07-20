# 📱 Social Media Copy & Outreach

This file contains copy-pasteable outreach content organized by channel.

---

## 🐦 Twitter / X

### 🧵 Launch Thread

Here is a copy-pasteable 6-tweet launch thread for Tessera. Each section is formatted to fit within Twitter's character limits.

#### Tweet 1 (Hook & Core Value)
> AWS MediaConvert pricing got you down? 💸
> 
> Build your own global-scale video ingestion and transcoding engine with Tessera. 
> 
> An open-source, cloud-agnostic alternative written in Go, NATS, and Redis.
> 
> Code & Docs: https://github.com/Ashutosh-Repos/Tessera 👇 (1/6)

---

#### Tweet 2 (Zero-Bandwidth API Gateway)
> 1/ Zero-Bandwidth Gateway 🚀
> 
> Traditional gateways bottleneck on raw video bytes. 
> 
> Tessera's Gateway authorizes sessions, fetches S3 multipart presigned URLs, and lets client browsers upload direct to S3/MinIO. Gateway traffic is pure JSON (<50MB RAM).
> 
> Check out the gateway implementation: https://github.com/Ashutosh-Repos/Tessera (2/6)

---

#### Tweet 3 (Etcd Ring & Coordinator Control Plane)
> 2/ Consistent Hash Ring Routing 🌐
> 
> Coordinators register with Etcd using 5s lease TTLs. 
> 
> An active FNV-1a consistent hash ring (150 virtual nodes per coordinator mapped across 1024 partitions) routes slicing and manifest compilation jobs deterministically, bypassing DB polling.
> 
> Read our hash ring logic here: https://github.com/Ashutosh-Repos/Tessera (3/6)

---

#### Tweet 4 (Faststart Stream Slicing & Dual Message Bus)
> 3/ Faststart Stream Slicing & Dual Bus 🎞️
> 
> Coordinators read only the first 1MB of the video to detect the MP4 `moov` atom. If faststart, it slices in-memory directly to S3 with zero disk write.
> 
> Select NATS JetStream (TLS 1.3 mTLS) or SQS at runtime: https://github.com/Ashutosh-Repos/Tessera (4/6)

---

#### Tweet 5 (Worker Fleet, cgroups & 4-Layer Recovery)
> 4/ OS-Level Guardrails & Self-Healing 🛡️
> 
> Workers run isolated FFmpeg subprocesses capped at 1.5GB memory & 50% CPU priority via Linux cgroups v2.
> 
> 4-layer recovery: NATS redelivery -> Coordinator DLQ -> GC daemon -> S3/Redis state reconstruction.
> 
> View our worker runtime executor: https://github.com/Ashutosh-Repos/Tessera (5/6)

---

#### Tweet 6 (Quickstart & Roadmaps)
> 5/ Spin up the entire distributed cluster locally in 60s:
> 
> ```bash
> git clone https://github.com/Ashutosh-Repos/Tessera.git
> cd Tessera
> chmod +x start.sh && ./start.sh
> ```
> 
> Check our GitHub Project board for roadmap items (GCP Pub/Sub, Kafka, GPU profiling) to see how you might contribute!
> 
> Repo link: https://github.com/Ashutosh-Repos/Tessera (6/6)

---

### 🧠 Building Tessera in Public (Current State & Roadmap)

Sharing the active, present-tense story of building Tessera builds trust and attracts developers who want to join an ongoing journey. Use these templates to post updates on X/Twitter.

#### 🧵 Active Build-in-Public Thread (Copy-Pasteable)

**Tweet 1 (Current State Hook):**
> I am currently building Tessera, an open-source distributed video engine, and sharing the build in public. 
> 
> The core architecture is live: stateless Go gateway, Etcd consistent hash ring, NATS sharding, and worker fleet.
> 
> Follow the build or read the code: https://github.com/Ashutosh-Repos/Tessera (1/5)

---

**Tweet 2 (What is working right now):**
> 2/ What is working today:
> - Zero-Bandwidth Gateway (<50MB RAM, S3 direct multipart uploads)
> - Faststart MP4 slicing in-memory from S3
> - Single-RTT Redis completion pipeline & progress stream multiplexing
> - Process isolation via Linux cgroups v2 (capping FFmpeg at 1.5GB/50% CPU)
> 
> Check out the codebase features: https://github.com/Ashutosh-Repos/Tessera (2/5)

---

**Tweet 3 (Building to Learn & Share):**
> 3/ I started this to learn real distributed systems and media engineering. 
> 
> Building in public means sharing the mistakes—like debugging running FFmpeg process leaks, resolving Redis universal client connection limits, and managing multi-region manifest compilation.
> 
> Let's discuss and review: https://github.com/Ashutosh-Repos/Tessera (3/5)

---

**Tweet 4 (The Roadmap & GitHub Project Board):**
> 4/ We are moving to the next phase, and I just set up our GitHub Project Board with open tasks:
> - Implementing the Google Cloud Pub/Sub queue driver
> - Building the pure-Go Kafka queue driver
> - Profiling Intel VAAPI/NVIDIA NVENC hardware acceleration
> 
> Check our open tasks: https://github.com/Ashutosh-Repos/Tessera (4/5)

---

**Tweet 5 (The Invitation):**
> 5/ The repo and active project boards are completely public. 
> 
> Whether you want to learn distributed Go patterns, SRE process isolation, or help optimize video codecs—let's build together! 
> 
> Check out the project board & code: https://github.com/Ashutosh-Repos/Tessera (5/5)

---

### 👤 X / Twitter Profile Setup

For maximum developer visibility, optimize your profile to clearly state what you build.

#### 1. Handle Suggestions (@...)
*   **Personal Dev Profile (Recommended for Build-in-Public):**
    *   `@ashutosh_codes`
    *   `@ashutosh_dev`
    *   `@ashutosh_io`
*   **Project-Specific Profile:**
    *   `@tessera_vod`
    *   `@tessera_engine`

#### 2. Display Name
*   `Ashutosh | Tessera VOD` *(Highly recommended: ties your personal name to the project)*
*   `Ashutosh (Building Tessera)`
*   `Tessera Engine` *(If using a project-only account)*

#### 3. Biography (Copy-Pasteable)
*   **Option A: Developer / Build-in-Public style (Recommended)**
    > Building Tessera (Open-source AWS MediaConvert alternative in Go) 🎬 \| Distributed Systems, Golang, Web3, & SRE. Shipping self-hosted dev tooling.
*   **Option B: Project-focused style**
    > Tessera: Cloud-agnostic, distributed video-on-demand (VOD) ingestion & transcoding engine in Go. Scale-out alternative to AWS MediaConvert. Open source.

#### 4. Link
*   `github.com/Ashutosh-Repos/Tessera`

#### 5. Banner & Visual Identity
*   **Avatar:** A high-quality photo of yourself (for personal) or the Tessera logo (for project).
*   **Banner:** A highly professional chalkboard-styled HLD architecture diagram featuring custom developer icons (shield for API Gateway, cylinders for Redis/S3, message pipeline for NATS, stacked servers/gears for Worker Fleet). It is formatted to exactly 1500x500 pixels (3:1 aspect ratio). You can find it here: [tessera_final_banner.png](file:///Users/ashutoshkumar/Desktop/Apple%20Project/lauch/tessera_final_banner.png).
    ![Tessera Blackboard HLD Banner](/Users/ashutoshkumar/Desktop/Apple Project/lauch/tessera_final_banner.png)

#### 6. Pinned Tweet Strategy
*   Post the **Launch Thread** (from the section above) and pin it to the top of your profile. This is the first thing developers will see when they visit your profile from search or retweets.

---

## 👽 Reddit & Developer Forums

### Option A: For `r/selfhosted` (Focus on cost, alternatives, and visual dashboards)
*   **Title:** `Tessera: An open-source distributed VOD engine & AWS MediaConvert alternative (Self-hostable, Go, NATS, Redis)`
*   **Body:**
```text
Hey r/selfhosted,

If you are building a video-centric app (educational platform, video sharing, private streaming server) or running video transcoding at scale, you've probably noticed that SaaS providers like AWS MediaConvert or Mux cost a fortune ($0.024/min average).

I’ve been working on Tessera, an open-source, cloud-agnostic alternative that runs on your own hardware (from a single $10 VPS to a clustered Kubernetes fleet). By eliminating per-minute licensing fees and scaling dynamically, it cuts infrastructure bills by 70–85%.

GitHub: https://github.com/Ashutosh-Repos/Tessera

### Key Architectural Highlights:
* **Zero-Bandwidth API Gateway:** The gateway daemon never touches video bytes (consumes <50MB RAM). The client PUTs binary parts directly to S3/MinIO via presigned URLs. Gateway only handles JSON control-plane logic.
* **Faststart Stream Slicing:** It reads only the first 1MB of a video to detect the MP4 `moov` atom. If optimized (faststart), it streams bytes from S3 straight to FFmpeg segmenters in memory, creating segment chunks with zero SSD writes.
* **Unified Developer Dashboard:** It comes with a Next.js Developer Customizer Studio and a Vite+React 19 SRE Admin Console to monitor worker queues, CPU/memory, and task states.
* **Manifest-Only Replication:** For multi-region setups, it only replicates playlist manifests (~10KB) across regions instead of heavy `.ts` segments, saving >99.99% on WAN cross-region egress bills.

### Quickstart (Platform-in-a-Box):
You can spin up the full cluster locally in about 60 seconds:
```bash
git clone https://github.com/Ashutosh-Repos/Tessera.git
cd Tessera
chmod +x start.sh && ./start.sh
```

This starts:
1. **API Gateway** on port `8080` (CORS-enabled REST edge)
2. **MinIO Object Console** on port `9001`
3. **Developer Customizer Portal** on port `3000` (Next.js studio)
4. **SRE Admin Console** on port `5173` (Vite telemetry dashboard)

Would love to know what you think, what features you'd like to see, or if you'd find this useful for your projects!
```

---

### Option B: For `r/golang` (Focus on concurrency patterns, Go standard library, and architecture)
*   **Title:** `Show r/golang: Tessera – A distributed video transcoding engine in Go (Etcd consistent hash ring, Redis Pipelines, NATS)`
*   **Body:**
```text
Hi everyone,

I wanted to share Tessera, a distributed video-on-demand (VOD) ingestion and transcoding engine I built in Go. It distributes FFmpeg slicing and manifest compilation jobs deterministically across a cluster of nodes.

All three layers (Gateway, Coordinator, Worker) are compiled from a single Go binary, selected by Cobra CLI (`video-engine server gateway | coordinator | worker`).

GitHub: https://github.com/Ashutosh-Repos/Tessera

### Interesting Go & Distributed Systems patterns used:
1. **Consistent Hash Ring via Etcd:** Coordinators keep a live membership list via Etcd leases. A virtual-node hash ring (150 virtual nodes per coordinator hashed via FNV-1a, mapped across 1024 partitions) routes jobs deterministically, bypassing DB polling.
2. **Single-RTT Redis Pipelines:** When a transcoding worker finishes a chunk, it updates bitmaps, hashes, and streams in a single roundtrip to Redis, minimizing network latency.
3. **SSE Progress Multiplexer:** SSE streams usually require one Redis connection per client. We built a Gateway multiplexer that runs a single Redis `XREAD BLOCK` loop, fanning out updates to subscriber channels. This collapses 50,000 concurrent client connections into 1 Redis connection per gateway node.
4. **Cgroup Process Isolation:** Spawns isolated FFmpeg subprocesses with memory limits and CPU weights on Linux (using `/sys/fs/cgroup/transcoder/task-[PID]`, capping memory at 1.5GB and cpu.weight at 50) and falls back to `renice +10` on macOS.

Looking for code reviews on the architecture, the Go concurrency model, or driving the CLI. 

Check out the code here: https://github.com/Ashutosh-Repos/Tessera
```

---

### Option C: For `r/devops` / `r/sre` (Focus on reliability, observability, and system tuning)
*   **Title:** `Show DevOps: Tessera – Distributed video engine with cgroups v2 limits, 4-tier failure recovery, and Prometheus metrics`
*   **Body:**
```text
Hey DevOps/SREs,

I wanted to share Tessera, a distributed video-on-demand engine built in Go. It transcodes large media files by slicing them into chunks and running parallel FFmpeg jobs across a fleet of stateless workers.

Since it runs on bare compute, we had to build robust SRE guardrails and observability in Go to handle unpredictable CPU/memory spikes and media corruption.

GitHub: https://github.com/Ashutosh-Repos/Tessera

### Reliability & Resource Tuning:
* **cgroups v2 Process Isolation:** Each FFmpeg subprocess is placed in its own cgroup on Linux, capping memory at 1.5GB and reducing CPU priority (`cpu.weight = 50`) to protect the node from noisy neighbor patterns. macOS falls back to `renice +10`.
* **4-Layer Self-Healing Pipeline:** Worker failover handles task failures gracefully: NATS redelivery (AckWait=30s) -> Coordinator DLQ with exponential backoff -> GC daemon sweep -> S3/Redis state reconstruction.
* **Disk Quota Safety:** Before executing, workers perform a `syscall.Statfs` check on the scratch directory. Files are size-limited (SIGKILL at 3GB), and an active watchdog SIGKILLs FFmpeg if output file size remains unchanged for 10 seconds.
* **Epoch Fencing:** To protect manifest writes from split-brain scenarios, coordinators validate ownership epochs in Redis. Stale coordinators are blocked from writing updates.
* **Observability:** Out-of-the-box support for 21 Prometheus metrics (across gateway, coordinators, and workers) and OpenTelemetry tracing with OTLP gRPC export and W3C traceparent correlation.

Would love to hear how you design resource isolation for compute-heavy workloads or handle failovers in your media pipelines!
```


