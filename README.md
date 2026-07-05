<div align="center">
  
# 🎥 Distributed VOD Engine

**A hyper-scalable, cloud-agnostic video transcoding platform designed for world-wide scale.**

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](#)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](#)

*An open-source, vendor-neutral alternative to AWS Elemental MediaConvert.*

</div>

---

## ⚡ Overview

The Distributed VOD Engine is a high-performance backend system designed to securely ingest, slice, and transcode massive video files at global scale. 

By utilizing a strict **Shared-Nothing Architecture** and **Pluggable Drivers**, the engine entirely avoids vendor lock-in. It runs flawlessly on a local laptop, a free-tier Oracle ARM instance, or an autoscaling Kubernetes cluster on AWS.

## 🚀 Quickstart (Platform-in-a-Box)

Want to run the entire distributed cluster locally on your laptop in 60 seconds?

```bash
# 1. Clone the repository
git clone https://github.com/your-org/distributed-transcoder.git
cd distributed-transcoder

# 2. Boot the engine and infrastructure
./start.sh
```

**What does this do?**
It uses Docker Compose to boot the entire stack:
*   **Infrastructure:** Redis, NATS JetStream, Etcd, and MinIO (Local S3).
*   **Engine:** The API Gateway, the Coordinator, and 2 Transcoding Workers.

You can instantly interact with the API at `http://localhost:8080` and the MinIO storage console at `http://localhost:9001`.

## 📚 Documentation

This project uses the industry-standard Diátaxis documentation framework. 

### [Tutorials (Learning)](docs/tutorials/)
*   [01. Running Locally & Uploading Videos](docs/tutorials/01-running-locally.md)
*   [02. Using the React UI SDK](docs/tutorials/02-using-the-sdk.md)

### [Architecture (Deep-Dive Explanations)](docs/architecture/)
*   [01. System Design & The 3 Tiers](docs/architecture/01-system-design.md)
*   [02. Lifecycle & Algorithms (Faststart Slicing, Redis Pipelines)](docs/architecture/02-lifecycle-and-algorithms.md)
*   [03. Global Federation (Multi-Region & Geo-DNS)](docs/architecture/03-global-federation.md)

### [Deployment Guides (How-To)](docs/deployment/)
*   [01. Cloud-Native Production (Kubernetes & KEDA)](docs/deployment/01-kubernetes-production.md)
*   [02. The "Zero-Cost Beast" (Oracle Cloud Free Tier)](docs/deployment/02-oracle-free-tier.md)

### [Reference](docs/reference/)
*   [01. Environment Variables Configuration](docs/reference/configuration.md)
*   [02. REST API Specification](docs/reference/api.md)

---
*Built for extreme concurrency. Powered by Go, NATS, and FFmpeg.*
