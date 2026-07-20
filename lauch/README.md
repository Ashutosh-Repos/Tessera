# 📢 Copy-Pasteable Launch Drafts for Tessera

Here are three tailored posts you can copy, edit, and share with the developer community right now.

---

## 1. Hacker News (Show HN)

*   **Submission Type:** Show HN
*   **Title:** `Show HN: Tessera – Open-source distributed VOD engine in Go`
*   **Link:** `https://github.com/Ashutosh-Repos/Tessera`
*   **Show HN Description / Text:**

```text
Hi HN,

We built Tessera (https://github.com/Ashutosh-Repos/Tessera) because we were tired of paying high per-minute transcoding fees to AWS Elemental MediaConvert and Bitmovin, as well as massive data transit bills. 

Tessera is a cloud-agnostic, distributed video-on-demand (VOD) ingestion and transcoding engine built in Go. It’s designed to run on your own compute (from a single VM sandbox to clustered Kubernetes pools), cutting infrastructure bills by 70–85%.

Here are a few architectural trade-offs and innovations we implemented:

1. Zero-Bandwidth API Gateway: Traditional gateways route heavy video bytes, creating network bottlenecks and data transit fees. Tessera’s Gateway only processes lightweight control-plane JSON (~5KB). It negotiates multipart upload sessions with S3/MinIO and hands cryptographically signed presigned URLs directly to the client browser, which uploads video chunks directly to storage.

2. Faststart Stream Slicing: Instead of downloading a massive 50GB video to local disk before slicing, the Coordinator fetches only the first 1MB of the video to detect the position of the MP4 `moov` container atom. If it's a Faststart video, it pipes the S3 socket stream directly in memory to FFmpeg chunking with zero disk usage.

3. Consensus Hash Ring: Coordinators register with Etcd consensus using a 5-second lease TTL. A virtual-node hash ring (150 v-nodes per coordinator across 1024 partitions) routes slicing and manifest compilation jobs deterministically, bypassing database polling bottlenecks. Epoch Fencing prevents stale coordinators from overwriting active playlists in Redis.

4. SSE Progress Multiplexing: SSE streams usually require one Redis connection per client. We built a Gateway multiplexer that runs a single Redis `XREAD BLOCK` loop, listening to all active progress streams and fanning out updates. This collapses 50k concurrent client connections into 1 Redis connection per gateway node.

5. OS Guardrails: FFmpeg processes run inside isolated Linux cgroups v2 (or `renice` on macOS), capping memory at 1.5GB and reducing CPU scheduling weight to prevent transcoding spikes from taking down other services on the host.

The repo includes a "Platform-in-a-Box" quickstart. Running `chmod +x start.sh && ./start.sh` spins up the Gateway, NATS JetStream, Redis Cluster, Etcd, MinIO, an SRE Admin Console, and a Developer Customizer Studio locally via Docker.

We'd love to hear your feedback on the architecture, the Go concurrency model, or any questions about self-hosted video infra!
```

---

## 2. Reddit

### Option A: For `r/selfhosted` (Focus on cost, alternative to AWS, and ease of running)
*   **Title:** `Tessera: An open-source distributed VOD engine & AWS MediaConvert alternative (Self-hostable, Go, NATS, Redis)`
*   **Body:**

```text
Hey r/selfhosted,

If you are building a video-centric app (educational platform, video sharing, private media server) or running video transcoding at scale, you've probably noticed that SaaS providers like AWS MediaConvert or Mux cost a fortune.

I’ve been working on Tessera, an open-source, cloud-agnostic alternative that runs on your own hardware (from a single $10 VPS to a clustered Kubernetes fleet). 

GitHub: https://github.com/Ashutosh-Repos/Tessera

### What makes it different?
* **Zero-Bandwidth Gateway:** Clients upload video chunks directly to S3/MinIO via presigned URLs. Video bytes never touch the gateway, saving bandwidth and preventing bottlenecks.
* **Faststart Stream Slicing:** It detects the MP4 `moov` atom using only the first 1MB of the video and slices it in memory without downloading the full video to disk first.
* **Unified Developer Stack:** It comes with a Next.js Developer Customizer Studio and a Vite+React 19 SRE Admin Console to monitor worker queues, CPU/memory, and task states.
* **Manifest-Only Replication:** For multi-region setups, it only replicates playlist manifests (~10KB) across regions instead of heavy `.ts` segments, saving >99.99% on WAN egress bills.

### Quickstart (Platform-in-a-Box):
You can spin up the full cluster locally in about 60 seconds:
```bash
git clone https://github.com/Ashutosh-Repos/Tessera.git
cd Tessera
chmod +x start.sh && ./start.sh
```

This starts:
1. **API Gateway** on port `8080`
2. **MinIO Console** on port `9001`
3. **Developer Customizer Portal** on port `3000`
4. **SRE Admin Console** on port `5173`

Would love to know what you think, what features you'd like to see, or if you'd find this useful for your projects!
```

### Option B: For `r/golang` (Focus on concurrency, Go patterns, and distributed systems)
*   **Title:** `Show r/golang: Tessera – A distributed video transcoding engine in Go (Etcd consistent hash ring, Redis Pipelines, NATS)`
*   **Body:**

```text
Hi everyone,

I wanted to share Tessera, a distributed video-on-demand (VOD) ingestion and transcoding engine I built in Go. It distributes FFmpeg slicing and manifest compilation jobs deterministically across a cluster of nodes.

GitHub: https://github.com/Ashutosh-Repos/Tessera

### Interesting Go & Distributed Systems patterns used:
1. **Consistent Hash Ring via Etcd:** Coordinators keep a live membership list via Etcd leases. A virtual-node hash ring (150 virtual nodes per coordinator mapped across 1024 partitions) routes jobs deterministically.
2. **Single-RTT Redis Pipelines:** When a transcoding worker finishes a chunk, it updates bitmaps, hashes, and streams in a single roundtrip to Redis.
3. **SSE Multiplexer:** Collapses thousands of client SSE connections into a single Redis connection per Gateway node using an async stream fanout.
4. **Linux cgroup Isolation:** Spawns isolated FFmpeg subprocesses with memory limits and CPU weights on Linux (using `renice` on macOS as a fallback) to ensure OS-level stability.

Looking for code reviews on the architecture, Go concurrency patterns, or driving the CLI. 

Check out the code here: https://github.com/Ashutosh-Repos/Tessera
```

---

## 3. Twitter / X (Thread Format)

**Tweet 1 (Hook):**
> AWS MediaConvert pricing got you down? 💸
> 
> Build your own global-scale video ingestion and transcoding engine with Tessera. 
> 
> An open-source, cloud-agnostic alternative written in Go, NATS, and Redis.
> 
> Check it out: https://github.com/Ashutosh-Repos/Tessera 👇 (1/5)

**Tweet 2:**
> 1/ Zero-Bandwidth Gateway 🚀
> 
> Traditional gateways get choked by raw video bytes. 
> 
> Tessera's Gateway authorizes sessions, requests S3 presigned URLs, and lets the client upload directly to S3/MinIO. 
> 
> Gateway traffic is strictly lightweight JSON. (2/5)

**Tweet 3:**
> 2/ Faststart Stream Slicing 🎞️
> 
> Tessera reads only the first 1MB of the video to detect the MP4 `moov` atom. 
> 
> If optimized, it streams bytes from S3 straight to FFmpeg segmenters in memory, creating segment chunks with zero disk writes. (3/5)

**Tweet 4:**
> 3/ Cost Savings 📉
> 
> By running on your own VMs or K8s nodes, Tessera reduces transcoding fees by 75–85% compared to proprietary SaaS.
> 
> Multi-region replication is manifest-only, saving >99.99% on WAN cross-region network egress bills! (4/5)

**Tweet 5:**
> 4/ Try it locally in 60s:
> 
> ```bash
> git clone https://github.com/Ashutosh-Repos/Tessera.git
> cd Tessera
> chmod +x start.sh && ./start.sh
> ```
> 
> Instantly spins up the Gateway, NATS, Redis Cluster, MinIO, SRE Admin Console, and Dev Customizer Studio! (5/5)
