import { useState } from 'react';
import { VideoUploader } from './components/VideoUploader';
import { VideoPlayer } from './components/VideoPlayer';
import { VideoTile } from './components/VideoTile';

function App() {
  const GATEWAY_URL = 'http://localhost:8080';
  const DEMO_HLS_URL = 'https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8';
  
  const [hlsUrl, setHlsUrl] = useState<string>(DEMO_HLS_URL);

  const demoTiles = [
    {
      id: 'tile-1',
      title: 'Building Hyper-Scalable Distributed Transcoding Fleets with Go & NATS JetStream',
      channelName: 'DeepMind Systems',
      channelAvatar: 'https://images.unsplash.com/photo-1534528741775-53994a69daeb?auto=format&fit=crop&w=120&q=80',
      views: '482K views',
      uploadedAt: '2 days ago',
      duration: '14:20',
      posterUrl: 'https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?auto=format&fit=crop&w=800&q=80',
      spriteUrl: 'https://raw.githubusercontent.com/vtt-demos/sprites/main/sample-sprite.jpg',
      badge: '4K',
      isVerified: true,
    },
    {
      id: 'tile-2',
      title: 'Low-Latency HLS & Consistent Hash Partitioning Deep Dive',
      channelName: 'Cloud Architecture Weekly',
      channelAvatar: 'https://images.unsplash.com/photo-1507003211169-0a1dd7228f2d?auto=format&fit=crop&w=120&q=80',
      views: '1.2M views',
      uploadedAt: '1 week ago',
      duration: '08:45',
      posterUrl: 'https://images.unsplash.com/photo-1550745165-9bc0b252726f?auto=format&fit=crop&w=800&q=80',
      previewVideoUrl: 'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerBlazes.mp4',
      badge: 'HD',
      isVerified: true,
    },
    {
      id: 'tile-3',
      title: 'S3 Cross-Region Replication & Zero-Loss Slicing Architecture',
      channelName: 'Streaming Engineering',
      channelAvatar: 'https://images.unsplash.com/photo-1517841905240-472988babdf9?auto=format&fit=crop&w=120&q=80',
      views: '95K views',
      uploadedAt: '4 hours ago',
      duration: '22:10',
      posterUrl: 'https://images.unsplash.com/photo-1526374965328-7f61d4dc18c5?auto=format&fit=crop&w=800&q=80',
      spriteUrl: 'https://raw.githubusercontent.com/vtt-demos/sprites/main/sample-sprite.jpg',
      badge: 'NEW',
      isVerified: false,
    }
  ];

  return (
    <div className="min-h-screen bg-black text-neutral-100 flex flex-col items-center justify-start py-12 px-4 sm:px-6 lg:px-8 font-sans selection:bg-neutral-800 selection:text-white">
      <div className="w-full max-w-5xl flex flex-col gap-12">
        
        {/* Header Section */}
        <header className="flex flex-col gap-2 border-b border-neutral-900 pb-8 text-center sm:text-left">
          <h1 className="text-2xl sm:text-3xl font-bold tracking-tight font-mono text-white flex items-center justify-center sm:justify-start gap-3">
            TESSERA // UI_SDK <span className="rounded bg-neutral-900 px-2 py-0.5 text-xs font-semibold text-neutral-400 border border-neutral-800">COMPONENT_SHOWCASE</span>
          </h1>
          <p className="text-xs sm:text-sm text-neutral-400 font-mono tracking-wide uppercase">
            Industry-standard React components for Video Upload, HLS Playback, and YouTube-style Preview Video Tiles
          </p>
        </header>
        
        {/* Main Sections */}
        <main className="flex flex-col gap-10">

          {/* Video Tile Gallery Section (YouTube Home Style) */}
          <section className="bg-black border border-neutral-900 rounded-xl p-6 sm:p-8 shadow-sm flex flex-col gap-6">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-neutral-900">
              <div>
                <h2 className="text-sm font-bold font-mono text-neutral-200 uppercase tracking-widest">&lt;VideoTile /&gt; GALLERY</h2>
                <p className="text-[11px] text-neutral-400 font-mono mt-0.5">YouTube Home Video Cards with Hover Sprite Flipbook & Video Preview</p>
              </div>
              <div className="flex flex-wrap gap-2">
                <span className="inline-flex items-center rounded border border-neutral-800 bg-neutral-950 px-2.5 py-0.5 text-[10px] font-mono text-neutral-400 uppercase tracking-wider">
                  Hover Sprite Preview
                </span>
                <span className="inline-flex items-center rounded border border-neutral-800 bg-neutral-950 px-2.5 py-0.5 text-[10px] font-mono text-neutral-400 uppercase tracking-wider">
                  Muted Video Preview
                </span>
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              {demoTiles.map(tile => (
                <VideoTile key={tile.id} {...tile} />
              ))}
            </div>
          </section>

          {/* Player Section */}
          <section className="bg-black border border-neutral-900 rounded-xl p-6 sm:p-8 shadow-sm flex flex-col gap-6">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-neutral-900">
              <div>
                <h2 className="text-sm font-bold font-mono text-neutral-200 uppercase tracking-widest">&lt;VideoPlayer /&gt;</h2>
                <p className="text-[11px] text-neutral-400 font-mono mt-0.5">Adaptive HLS player with real-time telemetry, speed controls, PiP & sprite scrubbing</p>
              </div>
              <div className="flex flex-wrap gap-2">
                <span className="inline-flex items-center rounded border border-neutral-800 bg-neutral-950 px-2.5 py-0.5 text-[10px] font-mono text-neutral-400 uppercase tracking-wider">
                  HLS Adaptive
                </span>
                <span className="inline-flex items-center rounded border border-neutral-800 bg-neutral-950 px-2.5 py-0.5 text-[10px] font-mono text-neutral-400 uppercase tracking-wider">
                  Telemetry Overlay
                </span>
              </div>
            </div>
            
            <VideoPlayer 
              hlsUrl={hlsUrl} 
              spriteUrl="https://raw.githubusercontent.com/vtt-demos/sprites/main/sample-sprite.jpg"
              spriteConfig={{ width: 160, height: 90, cols: 5, intervalSec: 5 }}
            />
            
            <p className="text-center text-[10px] text-neutral-500 font-mono mt-2 bg-neutral-950 py-3 rounded border border-neutral-900/60 uppercase tracking-wider">
              Press <kbd className="mx-1 px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-850 text-neutral-400">D</kbd> for telemetry · <kbd className="mx-1 px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-850 text-neutral-400 font-bold text-white">Hover Seekbar</kbd> for sprite scrubbing · <kbd className="mx-1 px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-850 text-neutral-400">F</kbd> for fullscreen · <kbd className="mx-1 px-1.5 py-0.5 rounded bg-neutral-900 border border-neutral-850 text-neutral-400">P</kbd> for PiP
            </p>
          </section>

          {/* Uploader Section */}
          <section className="bg-black border border-neutral-900 rounded-xl p-6 sm:p-8 shadow-sm flex flex-col gap-6">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-neutral-900">
              <div>
                <h2 className="text-sm font-bold font-mono text-neutral-200 uppercase tracking-widest">&lt;VideoUploader /&gt;</h2>
                <p className="text-[11px] text-neutral-400 font-mono mt-0.5">S3 Direct Multipart Uploader with Real-Time SSE Transcoding Telemetry</p>
              </div>
              <div className="flex flex-wrap gap-2">
                <span className="inline-flex items-center rounded border border-neutral-800 bg-neutral-950 px-2.5 py-0.5 text-[10px] font-mono text-neutral-400 uppercase tracking-wider">
                  S3 Multipart
                </span>
                <span className="inline-flex items-center rounded border border-neutral-800 bg-neutral-950 px-2.5 py-0.5 text-[10px] font-mono text-neutral-400 uppercase tracking-wider">
                  Real-Time SSE
                </span>
              </div>
            </div>
            <VideoUploader gatewayUrl={GATEWAY_URL} onUploadSuccess={(url) => setHlsUrl(url)} />
          </section>

        </main>
      </div>
    </div>
  );
}

export default App;
