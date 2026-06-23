# Research: Video Ingestion, Transcoding, and Live Streaming Architectures

This document details the systems architecture, network protocols, processing workflows, and delivery mechanisms used by modern scale video platforms (like YouTube and Apple TV+) to handle Video on Demand (VoD) and live streaming.

---

## 1. Video on Demand (VoD) Ingest & Processing Pipeline

When a user uploads a video file, it is processed asynchronously through a distributed MapReduce-style media pipeline.

```
┌────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│  Client Upload │ ────> │  Ingest Gateway │ ────> │ Video Segmenter │
│  (Tus / HTTP)  │       │  (VFS Storage)  │       │ (GOP-based cuts)│
└────────────────┘       └─────────────────┘       └────────┬────────┘
                                                            │
                                                            ▼
┌───────────────────────────────────────────────────────────┴────────┐
│               Distributed Task Coordinator / Queue                 │
│                                                                    │
│   ┌──────────────────────┐               ┌──────────────────────┐  │
│   │ Worker 1: 1080p VP9  │               │ Worker 3: 4K AV1     │  │
│   └──────────────────────┘               └──────────────────────┘  │
│   ┌──────────────────────┐               ┌──────────────────────┐  │
│   │ Worker 2: 720p H.264 │               │ Worker 4: 480p H.264 │  │
│   └──────────────────────┘               └──────────────────────┘  │
└───────────────────────────┬────────────────────────────────────────┘
                            │ (Transcoded Segment Outputs)
                            ▼
┌────────────────────────────────────────────────────────────────────┐
│                  HLS / DASH Manifest Compiler                      │
│      Generates playlist files (.m3u8 / .mpd) mapping chunks        │
└───────────────────────────┬────────────────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────────────────┐
│                    Google Global Cache (GGC)                       │
│              Edge CDNs closer to the client player                 │
└────────────────────────────────────────────────────────────────────┘
```

### 1.1 Ingestion Phase
* **Resumable Protocols (Tus)**: Large media files (e.g., 20GB raw container) are prone to network interruptions. Using the open **Tus protocol** (running over HTTP/1.1 or HTTP/2), file transfers are chunked and indexed. If the socket closes, the client queries the ingest gateway for the last successful offset and resumes without losing progress.
* **Unpacking & Demuxing**: The incoming media container (e.g., MP4, MKV, QuickTime) is demuxed. The audio tracks, video tracks, and closed captions are extracted into independent component streams.

### 1.2 Video Segmentation (Chunking)
* **GOP (Group of Pictures) Boundaries**: Videos cannot be cut at arbitrary second markers because inter-frame compression relies on reference frames. The segmenter parses the video to locate **Keyframes (I-frames / Intra-coded pictures)**.
* **Segment Slicing**: The segmenter cuts the video into short, self-contained segments (typically 2 to 5 seconds long) at I-frame boundaries. Since each segment begins with a keyframe, it has no dependencies on preceding or succeeding frames and can be decoded/transcoded independently.
* **Task Distribution**: Each chunk is assigned a unique identifier (e.g., `job_102_chunk_004`) and pushed onto a global task queue.

### 1.3 Distributed Transcoding & Codecs
Distributed worker nodes fetch chunks from the queue and transcode them in parallel to targets specified by the platform's profile engine:
* **H.264 / AVC**: The legacy fallback profile. Highly compatible with older devices, but has lower compression efficiency.
* **VP9**: Developed by Google. Used for HD and 4K streaming, offering a 35% reduction in size compared to H.264 at identical visual qualities.
* **AV1**: A modern, open, royalty-free codec. It provides up to 30% better compression than VP9, but requires heavy processing power (often leveraging dedicated ASIC/GPU chips in production clusters).
* **HEVC / H.265**: Primarily used in Apple environments for 4K/HDR content, providing hardware acceleration across iOS/macOS devices.

---

## 2. Adaptive Bitrate (ABR) Streaming & Delivery

Once transcoded, segments are served dynamically based on real-time client conditions.

### 2.1 Dynamic Playlists & Manifests
The output of the transcoding pipeline is not a single file, but a folder containing thousands of chunk segments (`.ts` or `.m4s` files) and manifest index files:
* **DASH (Dynamic Adaptive Streaming over HTTP)**: Generates a Media Presentation Description (`.mpd`) XML file.
* **HLS (HTTP Live Streaming)**: Generates Master Index and Media `.m3u8` playlist files.
* **Manifest Structure**:
  ```m3u8
  #EXTM3U
  #EXT-X-VERSION:3
  #EXT-X-TARGETDURATION:5
  #EXT-X-MEDIA-SEQUENCE:0
  #EXTINF:5.000000,
  segment_000_1080p.ts
  #EXTINF:5.000000,
  segment_001_1080p.ts
  ```

### 2.2 Client-Side ABR Tuning
The client-side player (e.g., Video.js, Shaka Player) loads the manifest:
1. It constantly estimates network throughput and measures player frame-drops.
2. When fetching the next 2-second segment, it chooses the bitrate track matching the available speed.
3. If throughput drops (e.g., moving into a tunnel), the player requests `segment_002_480p.ts` instead of `segment_002_1080p.ts`, avoiding playback interruption or buffer spinner stalls.

---

## 3. Live Streaming Architecture

Live streaming requires dynamic, low-latency ingestion, JIT (Just-in-Time) transcoding, and immediate push to CDNs.

### 3.1 Live Ingestion Protocols
* **RTMP (Real-Time Messaging Protocol)**: Legacy, runs over TCP. Reliable but introduces latency because it enforces ordered packet delivery, blocking the stream on single packet losses (Head-of-Line blocking).
* **SRT (Secure Reliable Transport)**: Modern protocol built on UDP. Includes low-latency packet recovery mechanisms, making it ideal for streaming over unstable internet connections.
* **WebRTC / WHIP**: WebRTC HTTP Ingestion Protocol. Enables sub-second ingestion latency, commonly used for interactive live shows.

### 3.2 Real-Time Transcoding & Low Latency Delivery
* **GPU Pipelines**: Real-time transcoders receive live chunk frames and use hardware-accelerated encoding (NVENC, Intel QuickSync, or Apple Videotoolbox) to scale and compress the frames with minimal delay.
* **Chunked CMAF (Common Media Application Format)**: Breaks standard segments down into smaller "chunks" (e.g., 200ms fragments). The CDN can stream these fragments to the client player before the complete 2-second segment is fully encoded, reducing latency to TV-broadcast levels (under 3 seconds).

---

## 4. Systems Engineering Challenges in Media Pipelines

When building or designing a distributed video processing platform, the following systems concepts must be resolved:

1. **GOP-Alignment Consistency**: If segment boundaries are not aligned perfectly across different transcoded resolutions (e.g., if 1080p is sliced at 2s but 720p is sliced at 2.1s), switching tracks during playback causes audio/video drifts and visible visual artifacts.
2. **Dynamic Work-Stealing Scheduling**: Transcoding time varies by chunk content complexity (high-motion action scenes take longer to compress than static frames). Schedulers must dynamically distribute chunks to avoid worker starvation or queue bottlenecks.
3. **Storage Tiering**: Raw segments must be hosted in high-speed, local caches (SSD) close to the transcoder workers. Once transcoded, outputs are uploaded to object stores (cold storage) and distributed to CDN networks.
4. **Deadlock & Failover Recovery**: If a worker node crashes mid-transcode, its task lease must expire via heartbeats, resetting the state of that segment chunk back to `PENDING` to trigger a reschedule.
