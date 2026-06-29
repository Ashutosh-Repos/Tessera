# Multi-Region Geo-Distributed Deployment Guide

This guide details the network routing, configuration architectures, bucket replication rules, and failover workflows required to deploy the Distributed VOD Transcoder system across multiple global geographical regions (e.g., US-East, EU-West, and AP-East).

---

## 1. Global Topology & Architecture

Our system uses a **Share-Nothing Regional Control Plane** to guarantee high availability. There are no synchronous database connections or transactions spanning WAN networks.

```
       [ Global User Client ]
                 │
                 ▼
       ┌───────────────────┐
       │ Geo-DNS / Anycast │
       └─────────┬─────────┘
                 │
      ┌──────────┴──────────┐
      ▼                     ▼
┌──────────────┐      ┌──────────────┐
│ US-East PoP  │      │ EU-West PoP  │
│ (Local S3)   │      │ (Local S3)   │
└──────┬───────┘      └──────┬───────┘
       │                     │
       └─────► [S3 CRR] ◄────┘
         (Manifests & Sentinels Only)
```

### 1.1. Network Entrypoint: Geo-DNS / Anycast Routing
* **Inbound Traffic Routing**: Use Geo-DNS (e.g., Route 53 Geolocation routing) or Anycast IP routing to map requests for `gateway.transcoder.company.com` to the nearest regional API Gateway load balancer.
* **Storage Ingest**: The Gateway generates S3 presigned URLs pointing strictly to the local region's S3 bucket hostname. Video chunks are sent directly to the local bucket, avoiding long-distance WAN latency.

---

## 2. Multi-Region Config Profiles

We maintain separate YAML configurations for each region. These configurations define the region tags, local databases, and messaging channels.

```carousel
```yaml
# configs/us-east-1.yaml
role: "gateway"
region: "us-east-1"
message_bus_provider: "nats"

redis:
  addrs:
    - "redis-us-east.internal:6379"
  password: "prod-redis-password"
  pool_size: 100

nats:
  urls:
    - "nats://nats-us-east.internal:4222"

etcd:
  endpoints:
    - "etcd-us-east.internal:2379"

object_store:
  endpoint: "s3.us-east-1.amazonaws.com"
  bucket: "apple-transcoder-us-east"
  region: "us-east-1"
  access_key: "aws-key-us-east"
  secret_key: "aws-secret-us-east"
  use_ssl: true

gateway:
  listen_addr: ":8080"
  jwt_secret: "jwt-secret-us-east"
  admin_api_key: "admin-secret-us-east"
  max_upload_size_gb: 50
  rate_limit_per_ip: 1000
  rate_limit_per_user: 5000

coordinator:
  partition_count: 1024
  slicing_semaphore: 50
  nats_shard_count: 4
  etcd_lease_ttl_sec: 5
  slicing_lock_ttl_sec: 10

worker:
  scratch_dir: "/tmp/scratch"
  min_disk_free_gb: 20
  watchdog_interval_sec: 10
  max_task_duration_min: 5
  max_temp_file_size_gb: 15
  concurrent_tasks: 8
  graceful_drain_sec: 300
```
<!-- slide -->
```yaml
# configs/eu-west-1.yaml
role: "gateway"
region: "eu-west-1"
message_bus_provider: "nats"

redis:
  addrs:
    - "redis-eu-west.internal:6379"
  password: "prod-redis-password"
  pool_size: 100

nats:
  urls:
    - "nats://nats-eu-west.internal:4222"

etcd:
  endpoints:
    - "etcd-eu-west.internal:2379"

object_store:
  endpoint: "s3.eu-west-1.amazonaws.com"
  bucket: "apple-transcoder-eu-west"
  region: "eu-west-1"
  access_key: "aws-key-eu-west"
  secret_key: "aws-secret-eu-west"
  use_ssl: true

gateway:
  listen_addr: ":8080"
  jwt_secret: "jwt-secret-eu-west"
  admin_api_key: "admin-secret-eu-west"
  max_upload_size_gb: 50
  rate_limit_per_ip: 1000
  rate_limit_per_user: 5000

coordinator:
  partition_count: 1024
  slicing_semaphore: 50
  nats_shard_count: 4
  etcd_lease_ttl_sec: 5
  slicing_lock_ttl_sec: 10

worker:
  scratch_dir: "/tmp/scratch"
  min_disk_free_gb: 20
  watchdog_interval_sec: 10
  max_task_duration_min: 5
  max_temp_file_size_gb: 15
  concurrent_tasks: 8
  graceful_drain_sec: 300
```
```

---

## 3. Storage Replication (CRR) Configuration

To maintain **Data Gravity**, we replicate only completed manifests and completion status indicators across region buckets. We explicitly exclude the raw source videos and intermediate `.ts` chunk files from cross-region WAN replication.

### 3.1. AWS S3 Cross-Region Replication (CRR) Policy
Apply this replication configuration JSON to the source S3 bucket in each region (e.g. `apple-transcoder-us-east` replicating to `apple-transcoder-eu-west`):

```json
{
  "Role": "arn:aws:iam::123456789012:role/S3TranscoderReplicationRole",
  "Rules": [
    {
      "ID": "ReplicateLightweightMetadataOnly",
      "Status": "Enabled",
      "Priority": 1,
      "Filter": {
        "And": {
          "Prefix": "jobs/",
          "Tags": [
            {
              "Key": "replicate",
              "Value": "true"
            }
          ]
        }
      },
      "Destination": {
        "Bucket": "arn:aws:s3:::apple-transcoder-eu-west",
        "StorageClass": "STANDARD"
      }
    }
  ]
}
```

### 3.2. Automated Manifest Metadata Tagging
The compiler daemons output manifest files to S3 upon segment completion. The developer must configure S3 Bucket Lifecycle rules or tag events to automatically attach the `"replicate": "true"` tag to these key suffixes:
* `**/master.m3u8`
* `**/manifest.mpd`
* `**/job_completed.json`

This tag triggers instant, low-latency AWS S3 storage replication to all other regional buckets globally.

---

## 4. Regional Isolation & Job Filtering

Because the manifests are replicated to all global buckets, a local regional bucket will contain jobs that belong to foreign regions. The system prevents coordinators from redundantly processing these jobs.

### 4.1. Regional Prefix Hashing
When the gateway creates a session, it prefixes the Job ID with the active region code:
```go
jobID := fmt.Sprintf("%s:%s", g.cfg.Region, uuid.New().String())
// e.g., "us-east-1:7ff8b548-c8ee-449e-b7d1-c27633f81e3a"
```

### 4.2. Coordinator Filtering Logic
During partition takeovers, node boots, or S3 scans, the coordinator reconstructs state using S3 listing but filters out foreign jobs:
```go
func (pm *PartitionManager) reconstructFromS3(ctx context.Context) {
    // ...
    for _, key := range keys {
        jobID := extractJobID(key)
        
        // Enforce Regional Isolation
        parts := strings.Split(jobID, ":")
        if len(parts) > 1 && parts[0] != pm.coord.cfg.Region {
            continue // skip jobs originating from other regions
        }
        
        // Reconstruct local active job...
    }
}
```

---

## 5. Global SRE Disaster Recovery & Failover

In the event of a complete datacenter or cloud region outage (e.g. `us-east-1` goes dark), follow these failover procedures.

### 5.1. Traffic Failover (Active-Passive Geo-DNS)
Update the Geo-DNS or Anycast Routing policies to route traffic away from the failed region:

```bash
# Example AWS CLI command to update geolocation latency routing policies
aws route53 change-resource-record-sets \
  --hosted-zone-id Z1D633PJN98OC \
  --change-batch file://failover-dns-policy.json
```

The client applications will automatically begin requesting sessions from the healthy region (e.g. `eu-west-1`).

### 5.2. In-Flight Job Failover
* **Uncompleted Jobs**: Jobs that were mid-transcoding in the failed region when it crashed are lost locally. The client application, upon detecting connection drops via the SSE connection, should automatically re-request an upload session, which will resolve to the healthy region.
* **Completed Playback Access**: Because completed manifests were already replicated via CRR to the healthy region's bucket before the outage, playback requests for all previously finished videos will continue to resolve successfully from the healthy region.
