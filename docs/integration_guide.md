# Tessera Developer Integration Guide

To integrate video streaming into your application, use the Tessera Gateway API to ingest videos and use the visual `ui-sdk` React components for uploading and playback.

---

## 1. REST API Integration (Ingress Lifecycle)

### Step 1: Create an Upload Session
Initialize the upload by sending the file metadata. The gateway returns a `job_id`, an upload session JWT, and parameters for chunked uploading.

- **Request**: `POST /api/jobs/upload-session`
```json
{
  "file_size_bytes": 1073741824,
  "file_name": "tutorial.mp4",
  "content_type": "video/mp4"
}
```

- **Response (200 OK)**:
```json
{
  "job_id": "us-east:550e8400-e29b-41d4-a716-446655440000",
  "session_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "upload_id": "multipart-upload-id-from-s3",
  "part_size": 52428800,
  "total_parts": 21,
  "progress_wss": "wss://gateway/progress/us-east:550e8400...?token=..."
}
```

---

### Step 2: Request Presigned PUT URLs
Request signed S3 upload URLs for the file chunks. You can batch requests (e.g., 10 parts at a time).

- **Request**: `POST /api/jobs/{job_id}/urls?start=1&count=10`
- **Headers**: `Authorization: Bearer <session_token>`

- **Response (200 OK)**:
```json
{
  "part_numbers": [1, 2, 3],
  "urls": [
    "https://s3-bucket/raw/source.mp4?partNumber=1&uploadId=...&Signature=...",
    "https://s3-bucket/raw/source.mp4?partNumber=2&uploadId=...&Signature=...",
    "https://s3-bucket/raw/source.mp4?partNumber=3&uploadId=...&Signature=..."
  ]
}
```

---

### Step 3: Upload Binary Parts Directly to S3
PUT each binary chunk directly to its matching presigned URL.
```bash
curl -X PUT --data-binary @chunk_001.mp4 "https://s3-bucket/raw/source.mp4?partNumber=1..."
```
*Note: Make sure to capture the `ETag` header returned by S3 for each chunk.*

---

### Step 4: Finalize the Upload
Tell Tessera to assemble the S3 chunks and kick off the transcoding pipeline.

- **Request**: `POST /api/jobs/{job_id}/complete`
- **Headers**: `Authorization: Bearer <session_token>`
```json
{
  "parts": [
    { "part_number": 1, "etag": "\"etag-1\"" },
    { "part_number": 2, "etag": "\"etag-2\"" }
  ]
}
```

- **Response (200 OK)**:
```json
{
  "status": "completed"
}
```

---

### Step 5: Listen to Real-Time Progress Stream
Connect to the Server-Sent Events (SSE) endpoint to display a progress bar and receive the final streaming URLs.

- **Request**: `GET /progress/{job_id}?token=<session_token>`
- **Response Headers**: `Content-Type: text/event-stream`
- **Events Output**:
```http
data: {"phase":"SLICING"}

data: {"phase":"TRANSCODING","completed":12,"total":60,"pct":20}

data: {"phase":"COMPLETED","hls_url":"https://s3/master.m3u8","dash_url":"https://s3/manifest.mpd","sprite":"https://s3/sprite.jpg","sprite_vtt":"https://s3/sprite.vtt","thumbnails":["https://s3/thumb_0.jpg","https://s3/thumb_1.jpg"],"width":1920,"height":1080,"fps":30,"duration":102.43}
```

---

### Step 6: Poll Status (Alternative to SSE)
If you do not want to use SSE, you can poll the job status directly from the Redis status hash.

- **Request**: `GET /api/jobs/{job_id}/status`
- **Response (200 OK)**:
```json
{
  "completed": "60",
  "duration": "102.43",
  "fps": "30",
  "height": "1080",
  "job_id": "us-east:550e8400-e29b-41d4-a716-446655440000",
  "last_updated": "1782200020",
  "owner_epoch": "1782200000000000000",
  "partition": "512",
  "sprite_key": "jobs/partition_512/job_us-east:550e8400.../sprite/sprite.jpg",
  "sprite_vtt": "jobs/partition_512/job_us-east:550e8400.../sprite/sprite.vtt",
  "state": "COMPLETED",
  "thumbnail_0": "jobs/partition_512/job_us-east:550e8400.../thumbnails/thumb_0.jpg",
  "thumbnail_1": "jobs/partition_512/job_us-east:550e8400.../thumbnails/thumb_1.jpg",
  "thumbnail_2": "jobs/partition_512/job_us-east:550e8400.../thumbnails/thumb_2.jpg",
  "total": "60",
  "width": "1920"
}
```

---

## 2. SRE & Admin Operations API

Admin endpoints require Authorization headers containing the configured `AdminAPIKey` value.

### 1. List Jobs (Paginated)
List all job statuses from the cluster.
- **Request**: `GET /api/admin/jobs?limit=50&offset=0`
- **Headers**: `Authorization: Bearer <admin_api_key>`
- **Response (200 OK)**:
```json
[
  {
    "job_id": "us-east:550e8400-e29b-41d4-a716-446655440000",
    "phase": "COMPLETED",
    "completed": 60,
    "total": 60,
    "owner_epoch": 17822000000000,
    "partition_id": 512,
    "last_updated": 1782200020
  }
]
```

### 2. Regional Health Status
Inspect pings for backing services (Redis, NATS, S3, Etcd) and active worker CPU/GPU/task load.
- **Request**: `GET /api/admin/regions`
- **Headers**: `Authorization: Bearer <admin_api_key>`
- **Response (200 OK)**:
```json
{
  "region": "us-east-1",
  "gateway_url": "http://:8080",
  "healthy": true,
  "services": {
    "redis": true,
    "nats": true,
    "s3": true,
    "etcd": true
  },
  "active_sockets": 420,
  "upload_count": 12,
  "dlq_depth": 0,
  "workers": [
    { "id": "worker-node-01", "cpu": 25, "gpu": 40, "tasks": 2 }
  ]
}
```

### 3. List Active Coordinators
Get the list of Coordinator nodes currently registered in the Etcd consensus ring.
- **Request**: `GET /api/admin/coordinators`
- **Headers**: `Authorization: Bearer <admin_api_key>`
- **Response (200 OK)**:
```json
[
  "coordinator-node-01",
  "coordinator-node-02"
]
```

---

## 3. Frontend React Component Integration (`ui-sdk`)

Tessera provides a package containing ready-to-use React components.

### 1. File Upload (`VideoUploader`)
Handles session initialization, parallel chunk uploads directly to S3/MinIO, and tracks progress.
```tsx
import { VideoUploader } from 'ui-sdk';

function UploadWidget() {
  return (
    <VideoUploader
      gatewayUrl="http://localhost:8080"
      onComplete={(result) => console.log('HLS Playback URL:', result.hls_url)}
      onError={(err) => console.error('Upload failed:', err)}
    />
  );
}
```

### 2. Video Player (`VideoPlayer`)
A custom HTML5 adaptive player built on `hls.js` with quality selection, speed controls, and seek buttons.
```tsx
import { VideoPlayer } from 'ui-sdk';

function PlayerWidget() {
  return (
    <VideoPlayer
      src="https://s3-bucket/jobs/partition_512/job_abc/master.m3u8"
      poster="https://s3-bucket/jobs/partition_512/job_abc/thumbnails/thumb_0.jpg"
      showOverlaySeekButtons={true}
      seekIntervalSec={10}
      className="custom-player-theme"
    />
  );
}
```

### 3. Video Feed Tile (`VideoTile`)
Perfect for media feeds. Plays a silent HLS stream on hover and shows preview duration.
```tsx
import { VideoTile } from 'ui-sdk';

function VideoCard() {
  return (
    <VideoTile
      hlsUrl="https://s3-bucket/jobs/partition_512/job_abc/master.m3u8"
      staticPoster="https://s3-bucket/jobs/partition_512/job_abc/thumbnails/thumb_0.jpg"
      title="Architecture Deep Dive"
      duration="14:32"
    />
  );
}
```
