# ☁️ Free Cloud Setup, Testing & Real-World Benchmarking Guide for Tessera

This guide provides a comprehensive research report and operational playbook for setting up, stress-testing, and extracting real-world performance benchmarks for the **Tessera Distributed VOD Transcoding Engine** using **100% free cloud tiers, trial credits, and free GPU environments**.

---

## 1. Cloud Provider Options & Strategy Matrix

| Cloud Provider & Option | Free Resources / Credits | Duration | Compute Specs | Hardware Accel Support | Best Use Case for Tessera |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Oracle Cloud (OCI)**<br>*Always Free Tier* | **1,500 OCPU hours / 9,000 GB hours per mo**<br>+ 200 GB Storage + 10 TB/mo Egress | **Lifetime** (Always Free) | 2 OCPUs (4 vCPUs) Ampere A1 (ARM64) + 12 GB RAM | CPU (`libx264`) ARM NEON optimized | **24/7 Persistent Cluster Test**<br>(Gateway + Coordinator + Worker + Redis + NATS + MinIO) |
| **Google Cloud (GCP)**<br>*Free Trial Account* | **$300 Free Credits** | **90 Days** | Flexible VMs (`n1-standard-4`, GKE, Cloud Run) | **NVIDIA T4 GPUs** (`h264_nvenc`) | **Real GPU vs CPU Speedup Benchmarks** & GKE autoscaling load tests |
| **DigitalOcean**<br>*Developer Credit* | **$200 Free Credits** | **60 Days** | Flexible Droplets / DOKS Kubernetes | CPU (`libx264`) | **Multi-Node Networking & SSE Telemetry Fanout** (<50MB gateway test) |
| **Lightning AI Studios**<br>*Community Tier* | **~15–20 Free GPU Hours / month** | **Monthly Reset** | Docker-native container environments | **NVIDIA T4 / L4 / A10G GPUs** | **Zero-config NVENC Transcode Speed Benchmarks** per segment |
| **Microsoft Azure**<br>*Free Account* | **$200 Credits (30 days)** + 12 Months Popular Services | **30 Days / 12 Mo** | B-series burstable VMs / AKS | CPU / GPU (`NCv3`) | SQS vs NATS bus provider comparison |

---

## 2. Top Recommended Setup Architectures

### 🌟 Architecture Strategy 1: The Hybrid Zero-Cost Enterprise Benchmark (Recommended)

Combine **Oracle Cloud Always Free** (persistent control plane) with **GCP Free Credits / Lightning AI** (ephemeral GPU compute workers):

```
                        ┌─────────────────────────────────────────┐
                        │      Client Upload / Load Tester        │
                        └────────────────────┬────────────────────┘
                                             │
                       ┌─────────────────────┴─────────────────────┐
                       ▼                                           ▼
┌─────────────────────────────────────────────┐   ┌──────────────────────────────────────────────┐
│  OCI Always Free Tenancy (Persistent 24/7)  │   │   GCP / Lightning AI (Ephemeral GPU)     │
│                                             │   │                                              │
│ [Gateway API] ──► [Coordinator] ──► [NATS]  │   │ ┌──────────────────────────────────────────┐ │
│       │                 │            │      │ │ │ Worker Node (NVIDIA T4 GPU / NVENC)    │ │ │
│       ▼                 ▼            ▼      │ │ └──────────────────────────────────────────┘ │
│   [Redis] ─────────── [Etcd] ─────► [MinIO] │   │                                              │
└─────────────────────────────────────────────┘   └──────────────────────────────────────────────┘
```

1. **Persistent Control Plane (OCI Always Free ARM64)**: Run the Gateway, Coordinator, Redis, Etcd, NATS, and MinIO 24/7 on your free 4 vCPU / 12 GB RAM Ampere instance.
2. **Ephemeral Transcode Workers (GCP / Lightning AI)**: Spin up NVIDIA T4 GPU instances only during benchmark test runs to capture high-throughput `h264_nvenc` transcode metrics.

---

### 🌟 Architecture Strategy 2: Single-Node "Platform-in-a-Box" (Simplicity First)

Deploy the entire stack via Docker Compose on an OCI Always Free ARM instance or a DigitalOcean / GCP trial VM:

```bash
# 1. SSH into your free cloud instance
# 2. Cross-compile or run via Docker Compose
git clone https://github.com/Ashutosh-Repos/Tessera.git
cd Tessera

# Build & launch all daemons + infrastructure
docker compose -f docker-compose.prod.yml up -d --build
```

---

## 3. Step-by-Step Benchmarking Playbook

### Step 1: Prepare Source Videos for Testing

Generate standardized synthetic video files directly on your cloud instance without downloading gigabytes of video:

```bash
# Generate 10-second 1080p Test Video (~5 MB)
ffmpeg -y -f lavfi -i testsrc=duration=10:size=1920x1080:rate=30 \
  -f lavfi -i sine=frequency=440:duration=10 \
  -c:v libx264 -preset fast -pix_fmt yuv420p -movflags +faststart test_10s_1080p.mp4

# Generate 5-Minute 1080p Test Video (~150 MB)
ffmpeg -y -f lavfi -i testsrc=duration=300:size=1920x1080:rate=30 \
  -f lavfi -i sine=frequency=440:duration=300 \
  -c:v libx264 -preset fast -pix_fmt yuv420p -movflags +faststart test_5m_1080p.mp4
```

---

### Step 2: Run Automated Benchmark Load Test

Execute this bash script to run an end-to-end test and print precise timing metrics:

```bash
#!/usr/bin/env bash
set -e

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
TEST_FILE="test_5m_1080p.mp4"
FILE_SIZE=$(stat -c%s "$TEST_FILE" 2>/dev/null || stat -f%z "$TEST_FILE")

echo "=================================================="
echo "🎬 Tessera Cloud Benchmark Harness"
echo "=================================================="
echo "Target Gateway : $GATEWAY_URL"
echo "Source File    : $TEST_FILE ($FILE_SIZE bytes)"
echo "--------------------------------------------------"

START_TIME=$(date +%s%N)

# 1. Create Session
echo "[1/5] Creating Upload Session..."
SESSION_RESP=$(curl -s -X POST "$GATEWAY_URL/api/jobs/upload-session" \
  -H "Content-Type: application/json" \
  -d "{\"file_size_bytes\": $FILE_SIZE, \"file_name\": \"$TEST_FILE\", \"content_type\": \"video/mp4\"}")

JOB_ID=$(echo "$SESSION_RESP" | jq -r '.job_id')
TOKEN=$(echo "$SESSION_RESP" | jq -r '.session_token')

echo "      Job ID: $JOB_ID"

# 2. Get Presigned URL
echo "[2/5] Requesting Presigned PUT URL..."
URL_RESP=$(curl -s -X POST "$GATEWAY_URL/api/jobs/$JOB_ID/urls?start=1&count=1" \
  -H "Authorization: Bearer $TOKEN")

PUT_URL=$(echo "$URL_RESP" | jq -r '.urls[0]')

# 3. Direct Upload
echo "[3/5] Direct Uploading Chunk to Object Storage..."
UPLOAD_START=$(date +%s%N)
ETAG=$(curl -s -X PUT "$PUT_URL" \
  -H "Content-Type: video/mp4" \
  --data-binary "@$TEST_FILE" \
  -D - | grep -i ETag | awk '{print $2}' | tr -d '\r"')
UPLOAD_END=$(date +%s%N)
UPLOAD_MS=$(( (UPLOAD_END - UPLOAD_START) / 1000000 ))
echo "      Chunk Uploaded in ${UPLOAD_MS}ms (ETag: $ETAG)"

# 4. Complete Session
echo "[4/5] Completing Upload..."
curl -s -X POST "$GATEWAY_URL/api/jobs/$JOB_ID/complete" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"parts\": [{\"part_number\": 1, \"etag\": \"$ETAG\"}]}"

# 5. Poll Transcode Status
echo "[5/5] Transcoding Pipeline In Progress... Polling Status..."
STATUS=""
while [ "$STATUS" != "completed" ] && [ "$STATUS" != "failed" ]; do
  sleep 1
  STATUS_RESP=$(curl -s "$GATEWAY_URL/api/jobs/$JOB_ID/status")
  STATUS=$(echo "$STATUS_RESP" | jq -r '.state // .phase')
  COMPLETED=$(echo "$STATUS_RESP" | jq -r '.completed // 0')
  TOTAL=$(echo "$STATUS_RESP" | jq -r '.total // 0')
  echo "      [$(date +%T)] State: $STATUS | Progress: $COMPLETED/$TOTAL tasks"
done

END_TIME=$(date +%s%N)
TOTAL_SEC=$(( (END_TIME - START_TIME) / 1000000000 ))

echo "--------------------------------------------------"
echo "✅ BENCHMARK COMPLETE"
echo "   Total End-to-End Pipeline Latency: ${TOTAL_SEC}s"
echo "   Final Job State: $STATUS"
echo "=================================================="
```

---

## 4. Primary Metrics to Measure & Report

Extract metrics from Prometheus (`http://<node-ip>:9090/metrics`) to compile your benchmark report:

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                              TESSERA BENCHMARK SCORECARD                               │
├─────────────────────────────────────┬──────────────────────┬───────────────────────────┤
│ Metric Name                         │ Target / Threshold   │ Measured Value            │
├─────────────────────────────────────┼──────────────────────┼───────────────────────────┤
│ Transcode Duration / Chunk (CPU)    │ < 8.0s per 5s chunk  │ _________________________ │
│ Transcode Duration / Chunk (NVENC)  │ < 1.5s per 5s chunk  │ _________________________ │
│ Faststart Slicing Duration          │ < 3.0s (5-min video) │ _________________________ │
│ Gateway Memory (1,000 SSE Clients) │ < 50 MB              │ _________________________ │
│ Gateway Presigned URL Latency (p99) │ < 100 ms             │ _________________________ │
│ NATS Queue Processing Latency       │ < 50 ms              │ _________________________ │
│ Redis Pipeline Roundtrip            │ < 2 ms               │ _________________________ │
└─────────────────────────────────────┴──────────────────────┴───────────────────────────┘
```

---

## 5. Cost Control & Safeguards

1. **Set $0.01 Billing Alerts**: Configure budget alerts in GCP, Azure, and AWS to ensure free trial credits aren't accidentally breached.
2. **Shut Down GPU Workers**: Tear down GPU instances immediately after benchmarking to preserve trial credits.
3. **Use ARM64 on OCI**: Oracle's Always Free ARM Ampere allocation has **zero expiration date**, making it the safest long-term testing sandbox.
