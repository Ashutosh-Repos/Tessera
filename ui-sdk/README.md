# 📦 @distributed-transcoder/ui-sdk

The **Tessera UI SDK** is a production-grade React & TypeScript component library providing high-performance video uploading, real-time progress streaming, adaptive HLS/DASH video playback, and sprite thumbnail scrubbers.

---

## 🚀 Installation

```bash
npm install @distributed-transcoder/ui-sdk
# or
pnpm add @distributed-transcoder/ui-sdk
```

---

## 🛠️ Components & Usage

### 1. `VideoUploader`
Handles chunked client-side multipart S3 uploads with automatic retry, presigned URL batching, and real-time SSE progress updates.

```tsx
import React from 'react';
import { VideoUploader } from '@distributed-transcoder/ui-sdk';

export function UploadPage() {
  return (
    <VideoUploader
      apiBaseUrl="https://api.tessera.io"
      onUploadStart={(jobId) => console.log('Upload started:', jobId)}
      onProgress={(pct) => console.log(`Progress: ${pct}%`)}
      onComplete={(result) => {
        console.log('HLS Playlist:', result.hlsUrl);
        console.log('DASH Manifest:', result.dashUrl);
      }}
      onError={(err) => console.error('Upload failed:', err)}
    />
  );
}
```

---

### 2. `VideoPlayer`
An adaptive video player integrating HLS.js with automatic bitrate switching (1080p, 720p, 480p), timeline sprite preview scrubbing, keyboard shortcuts, and responsive layouts.

```tsx
import React from 'react';
import { VideoPlayer } from '@distributed-transcoder/ui-sdk';

export function WatchPage() {
  return (
    <VideoPlayer
      src="https://cdn.tessera.io/jobs/partition_5/job_abc/hls/master.m3u8"
      poster="https://cdn.tessera.io/jobs/partition_5/job_abc/poster.jpg"
      spriteConfig={{
        spriteUrl: "https://cdn.tessera.io/jobs/partition_5/job_abc/sprite.jpg",
        vttUrl: "https://cdn.tessera.io/jobs/partition_5/job_abc/sprite.vtt"
      }}
      autoplay={false}
      controls={true}
    />
  );
}
```

---

### 3. `VideoTile`
A card component for video feeds, displaying hover animated previews, duration badges, and playback triggers.

```tsx
import React from 'react';
import { VideoTile } from '@distributed-transcoder/ui-sdk';

export function VideoGrid() {
  return (
    <VideoTile
      title="Sample 4K Nature Clip"
      duration="05:20"
      thumbnailUrl="https://cdn.tessera.io/jobs/thumb.jpg"
      hlsUrl="https://cdn.tessera.io/jobs/master.m3u8"
      onClick={() => alert('Play video')}
    />
  );
}
```

---

## 🎨 Styling & Customization

The SDK comes with built-in Tailwind CSS support and allows overriding class names via props:

```tsx
<VideoPlayer
  src="https://cdn.tessera.io/master.m3u8"
  classNames={{
    container: "rounded-2xl shadow-2xl border border-white/10",
    controls: "bg-black/60 backdrop-blur-md",
  }}
/>
```
