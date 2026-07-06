"use client";

import React, { useState, useEffect, useRef } from "react";
import { 
  Server, 
  UploadCloud, 
  Play, 
  Pause,
  Cpu, 
  Settings, 
  Terminal, 
  Copy, 
  Check, 
  Plus, 
  Trash2, 
  Activity, 
  Layers, 
  Network, 
  BookOpen, 
  ArrowRight,
  Database,
  Globe,
  Volume2,
  VolumeX,
  Maximize,
  SlidersHorizontal,
  ChevronDown
} from "lucide-react";
import Hls from "hls.js";

export default function Home() {
  // ==========================================
  // 1. CONSISTENT HASH RING ALGORITHM
  // ==========================================
  const [nodes, setNodes] = useState<string[]>([
    "coordinator-us-east-1a",
    "coordinator-us-east-1b",
    "coordinator-eu-west-1a"
  ]);
  const [newNodeName, setNewNodeName] = useState<string>("");
  const [activePartition, setActivePartition] = useState<number | null>(null);
  const [rebalanceLog, setRebalanceLog] = useState<string[]>([
    "Initializing Consistent Hashing Ring topology...",
    "Registered partition P0 at 0°",
    "Registered partition P1 at 45°",
    "Registered partition P2 at 90°",
    "Registered partition P3 at 135°",
    "Registered partition P4 at 180°",
    "Registered partition P5 at 225°",
    "Registered partition P6 at 270°",
    "Registered partition P7 at 315°"
  ]);

  const getNodeAngle = (nodeName: string): number => {
    let hash = 2166136261;
    for (let i = 0; i < nodeName.length; i++) {
      hash ^= nodeName.charCodeAt(i);
      hash += (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
    }
    return Math.abs(hash % 360);
  };

  const getPartitionOwner = (partIdx: number) => {
    if (nodes.length === 0) return "none";
    const partAngle = partIdx * 45;

    const nodeMappings = nodes.map(name => ({
      name,
      angle: getNodeAngle(name)
    }));

    nodeMappings.sort((a, b) => a.angle - b.angle);

    for (const node of nodeMappings) {
      if (node.angle >= partAngle) {
        return node.name;
      }
    }
    return nodeMappings[0].name;
  };

  const addNode = () => {
    const name = newNodeName.trim();
    if (!name) return;
    if (nodes.includes(name)) return;

    const angle = getNodeAngle(name);
    const updated = [...nodes, name];
    setNodes(updated);
    setNewNodeName("");
    setRebalanceLog(prev => [
      `Node '${name}' registered lease. Hashed to ${angle}°`,
      `Rebalancing 8 partition slots across ${updated.length} active nodes...`,
      ...prev
    ]);
  };

  const removeNode = (target: string) => {
    if (nodes.length <= 1) {
      setRebalanceLog(prev => ["Cannot remove last node. Ring must maintain consensus.", ...prev]);
      return;
    }
    const updated = nodes.filter(n => n !== target);
    setNodes(updated);
    setRebalanceLog(prev => [
      `Fenced node '${target}'. Lease expired.`,
      `Rebalancing 8 partition slots across ${updated.length} active nodes...`,
      ...prev
    ]);
  };

  // ==========================================
  // 2. DYNAMIC COMPONENT CUSTOMIZER
  // ==========================================
  const [selectedComp, setSelectedComp] = useState<"uploader" | "player" | "tile">("uploader");
  
  // Customizer styling props state
  const [uploaderTitle, setUploaderTitle] = useState<string>("Ingest Video Segment");
  const [bgColor, setBgColor] = useState<string>("#09090b");
  const [borderColor, setBorderColor] = useState<string>("#27272a");
  const [btnBgColor, setBtnBgColor] = useState<string>("#ffffff");
  const [btnTextColor, setBtnTextColor] = useState<string>("#000000");
  const [borderRadius, setBorderRadius] = useState<number>(8); // in px
  const [paddingSize, setPaddingSize] = useState<number>(24); // in px
  
  const [playerAccent, setPlayerAccent] = useState<string>("#ffffff");
  const [playerAspectRatio, setPlayerAspectRatio] = useState<"16/9" | "4/3" | "1/1">("16/9");
  
  // Extended player styling controls
  const [controlsMode, setControlsMode] = useState<"overlay" | "below">("overlay");
  const [controlsBg, setControlsBg] = useState<string>("#000000");
  const [controlsBgOpacity, setControlsBgOpacity] = useState<number>(70); // percent
  const [controlsIconColor, setControlsIconColor] = useState<string>("#ffffff");
  const [progressBarColor, setProgressBarColor] = useState<string>("#ffffff");
  const [progressBarHeight, setProgressBarHeight] = useState<number>(3); // px
  const [playerBorderRadius, setPlayerBorderRadius] = useState<number>(8); // px
  const [showControlsOnHover, setShowControlsOnHover] = useState<boolean>(true);

  // VideoTile states
  const [tileTitle, setTileTitle] = useState<string>("Hyper-Scalable Transcoding Fleets");
  const [tileChannel, setTileChannel] = useState<string>("DeepMind Systems");
  const [tileBadge, setTileBadge] = useState<string>("4K");
  const [tilePreviewMode, setTilePreviewMode] = useState<"sprite" | "video">("sprite");

  // Mock interactive simulation actions
  const [mockFileName, setMockFileName] = useState<string>("");
  const [mockUploadProgress, setMockUploadProgress] = useState<number>(-1);
  const [mockJobState, setMockJobState] = useState<string>("");

  const handleMockUpload = () => {
    setMockFileName("raw_camera_source.mp4");
    setMockUploadProgress(0);
    setMockJobState("UPLOADING_CHUNKS");
  };

  useEffect(() => {
    if (mockUploadProgress >= 0 && mockUploadProgress < 100) {
      const timer = setTimeout(() => {
        setMockUploadProgress(prev => prev + 25);
      }, 600);
      return () => clearTimeout(timer);
    } else if (mockUploadProgress === 100) {
      setMockJobState("COMPILING_HLS");
      const timer = setTimeout(() => {
        setMockJobState("COMPLETED");
      }, 1000);
      return () => clearTimeout(timer);
    }
  }, [mockUploadProgress]);

  const resetMockUpload = () => {
    setMockFileName("");
    setMockUploadProgress(-1);
    setMockJobState("");
  };

  const [isCodeCopied, setIsCodeCopied] = useState<boolean>(false);

  // ==========================================
  // 3. REAL WORKING HLS PLAYER & TELEMETRY
  // ==========================================
  const videoRef = useRef<HTMLVideoElement>(null);
  const [hlsInstance, setHlsInstance] = useState<Hls | null>(null);
  const [isPlaying, setIsPlaying] = useState<boolean>(false);
  const [isMuted, setIsMuted] = useState<boolean>(false);
  const [currentTime, setCurrentTime] = useState<number>(0);
  const [duration, setDuration] = useState<number>(0);
  
  // Adaptive streaming levels
  const [hlsLevels, setHlsLevels] = useState<{ id: number; name: string; bitrate: number }[]>([]);
  const [currentHlsLevel, setCurrentHlsLevel] = useState<number>(-1); // -1 = Auto
  const [showQualitySelector, setShowQualitySelector] = useState<boolean>(false);

  // Telemetry histories for SVG Sparklines (capped at 30 entries)
  const [bufferHistory, setBufferHistory] = useState<number[]>(new Array(30).fill(0));
  const [bitrateHistory, setBitrateHistory] = useState<number[]>(new Array(30).fill(0));

  const [currentBufferSec, setCurrentBufferSec] = useState<number>(0);
  const [currentBitrateKbps, setCurrentBitrateKbps] = useState<number>(0);

  // Stable public adaptive test HLS stream
  const hlsSourceUrl = "https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8";

  useEffect(() => {
    if (selectedComp !== "player") {
      // Destroy player when switching away
      if (hlsInstance) {
        hlsInstance.destroy();
        setHlsInstance(null);
      }
      setIsPlaying(false);
      return;
    }

    const video = videoRef.current;
    if (!video) return;

    let hls: Hls | null = null;

    if (Hls.isSupported()) {
      hls = new Hls({
        enableWorker: true,
        startLevel: -1, // Auto
      });

      hls.loadSource(hlsSourceUrl);
      hls.attachMedia(video);

      hls.on(Hls.Events.MANIFEST_PARSED, (_, data) => {
        const parsedLevels = data.levels.map((lvl, idx) => ({
          id: idx,
          name: `${lvl.height}p`,
          bitrate: Math.round(lvl.bitrate / 1000)
        }));
        setHlsLevels(parsedLevels);
      });

      hls.on(Hls.Events.LEVEL_SWITCHED, (_, data) => {
        setCurrentHlsLevel(data.level);
      });

      setHlsInstance(hls);
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = hlsSourceUrl;
    }

    return () => {
      if (hls) {
        hls.destroy();
      }
    };
  }, [selectedComp]);

  // Video event handlers
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const onPlay = () => setIsPlaying(true);
    const onPause = () => setIsPlaying(false);
    const onTimeUpdate = () => setCurrentTime(video.currentTime);
    const onLoadedMetadata = () => setDuration(video.duration);
    const onVolumeChange = () => setIsMuted(video.muted);

    video.addEventListener("play", onPlay);
    video.addEventListener("pause", onPause);
    video.addEventListener("timeupdate", onTimeUpdate);
    video.addEventListener("loadedmetadata", onLoadedMetadata);
    video.addEventListener("volumechange", onVolumeChange);

    return () => {
      video.removeEventListener("play", onPlay);
      video.removeEventListener("pause", onPause);
      video.removeEventListener("timeupdate", onTimeUpdate);
      video.removeEventListener("loadedmetadata", onLoadedMetadata);
      video.removeEventListener("volumechange", onVolumeChange);
    };
  }, [selectedComp]);

  // Telemetry Polling (every 500ms)
  useEffect(() => {
    if (selectedComp !== "player") return;

    const interval = setInterval(() => {
      const video = videoRef.current;
      if (!video) return;

      // 1. Calculate Buffer occupancy
      let bufferSec = 0;
      if (video.buffered.length > 0) {
        for (let i = 0; i < video.buffered.length; i++) {
          if (video.currentTime >= video.buffered.start(i) && video.currentTime <= video.buffered.end(i)) {
            bufferSec = video.buffered.end(i) - video.currentTime;
            break;
          }
        }
      }
      setCurrentBufferSec(Math.round(bufferSec * 10) / 10);

      // 2. Fetch current bitrate from active level
      let bitrateKbps = 0;
      if (hlsInstance) {
        const activeLevel = hlsInstance.levels[hlsInstance.currentLevel];
        if (activeLevel) {
          bitrateKbps = Math.round(activeLevel.bitrate / 1000);
        }
      } else {
        // Fallback for native Safari
        bitrateKbps = 2400;
      }
      setCurrentBitrateKbps(bitrateKbps);

      // 3. Shift history arrays
      setBufferHistory(prev => {
        const next = [...prev.slice(1), bufferSec];
        return next;
      });

      setBitrateHistory(prev => {
        const next = [...prev.slice(1), bitrateKbps];
        return next;
      });
    }, 500);

    return () => clearInterval(interval);
  }, [selectedComp, hlsInstance]);

  const togglePlayback = () => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) {
      video.play().catch(() => {});
    } else {
      video.pause();
    }
  };

  const toggleMute = () => {
    const video = videoRef.current;
    if (!video) return;
    video.muted = !video.muted;
  };

  const selectQuality = (levelId: number) => {
    if (!hlsInstance) return;
    hlsInstance.currentLevel = levelId; // -1 = Auto
    setCurrentHlsLevel(levelId);
    setShowQualitySelector(false);
  };

  const formatTime = (sec: number) => {
    if (isNaN(sec) || !isFinite(sec)) return "00:00";
    const mins = Math.floor(sec / 60);
    const secs = Math.floor(sec % 60);
    return `${mins.toString().padStart(2, "0")}:${secs.toString().padStart(2, "0")}`;
  };

  const generatedJSXCode = selectedComp === "uploader" 
    ? `import { VideoUploader } from '@distributed-transcoder/ui-sdk';

function App() {
  return (
    <VideoUploader 
      gatewayUrl="http://localhost:8080"
      maxSizeGb={5}
      title="${uploaderTitle}"
      style={{
        backgroundColor: "${bgColor}",
        borderColor: "${borderColor}",
        borderRadius: "${borderRadius}px",
        padding: "${paddingSize}px"
      }}
      buttonStyle={{
        backgroundColor: "${btnBgColor}",
        color: "${btnTextColor}"
      }}
      onUploadComplete={(job) => {
        console.log("Upload completed, Job ID:", job.job_id);
      }}
    />
  );
}`
    : selectedComp === "player"
    ? `import { VideoPlayer } from '@distributed-transcoder/ui-sdk';

function App() {
  return (
    <VideoPlayer 
      hlsUrl="${hlsSourceUrl}"
      spriteUrl="https://raw.githubusercontent.com/vtt-demos/sprites/main/sample-sprite.jpg"
      spriteConfig={{ width: 160, height: 90, cols: 5, intervalSec: 5 }}
      aspectRatio="${playerAspectRatio}"
      accentColor="${playerAccent}"
      autoPlay={false}
      borderRadius={${playerBorderRadius}}
      controlsPosition="${controlsMode}"${controlsMode === "overlay" ? `\n      showControlsOnHover={${showControlsOnHover}}` : ""}
      onPlay={() => console.log("HLS stream playback started")}
    />
  );
}`
    : `import { VideoTile } from '@distributed-transcoder/ui-sdk';

function App() {
  return (
    <VideoTile 
      title="${tileTitle}"
      channelName="${tileChannel}"
      views="482K views"
      uploadedAt="2 days ago"
      duration="14:20"
      posterUrl="https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe"
      ${tilePreviewMode === "sprite" ? 'spriteUrl="https://raw.githubusercontent.com/vtt-demos/sprites/main/sample-sprite.jpg"' : 'previewVideoUrl="https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerBlazes.mp4"'}
      badge="${tileBadge}"
      isVerified={true}
      onClick={() => console.log("Video tile clicked")}
    />
  );
}`;

  // ==========================================
  // 4. ARCHITECTURE PIPELINE MAP
  // ==========================================
  const [activeArchStage, setActiveArchStage] = useState<string>("ingest");
  const stageExplanations: Record<string, { title: string; desc: string; metrics: string }> = {
    ingest: {
      title: "1. Client Direct Storage Ingestion",
      desc: "Gateway generates JWT-signed AWS S3 presigned URLs. The client uploads chunks directly to S3-compatible storage, enforcing local Data Gravity and eliminating network ingress bottleneck.",
      metrics: "Ingest Limit: 50GB/job | WAN hop latency: 0ms"
    },
    coordinator: {
      title: "2. Consistent Hash Ring Slicing",
      desc: "etcd tracks registered coordinator leases. Upload events are dispatched via partition keys. The lease-owning coordinator takes an etcd lock, relocates faststart index, and slices the MP4 into 5-second GOP-aligned chunks.",
      metrics: "Ring Partitions: 1024 slots | Lock TTL: 5s"
    },
    messagebus: {
      title: "3. Sharded Task Dispatch (NATS JetStream)",
      desc: "Slices are queued onto sharded NATS streams. Subscriptions are isolated per region to ensure that workers never pull transcode tasks from foreign locations.",
      metrics: "NATS Shards: 4 | AckWait: 30s"
    },
    workers: {
      title: "4. Autonomous Parallel Worker Fleets",
      desc: "Workers consume tasks concurrently, checking local disk quota beforehand. Chunks are transcoded to target presets (1080p, 720p, 480p) via ffmpeg sub-processes and uploaded to temporary S3 paths.",
      metrics: "Concurrency Limit: 4/worker | HW-Accel: Videotoolbox/NVENC"
    },
    compilation: {
      title: "5. Manifest Compiling & S3 CRR Sync",
      desc: "Once all sub-segments finish, the coordinator compiles target M3U8/MPD manifests. AWS S3 Cross-Region Replication (CRR) replicates ONLY completed master playlist sentinels to remote global buckets.",
      metrics: "CRR sync time: <2s | Raw segment WAN size: 0B"
    }
  };

  // ==========================================
  // 5. EMBEDDED SYSTEM MANUALS
  // ==========================================
  const [docTab, setDocTab] = useState<"quickstart" | "deployment" | "master">("quickstart");

  return (
    <main className="relative min-h-screen bg-black text-foreground overflow-x-hidden">
      <div className="absolute inset-0 grid-bg opacity-40 pointer-events-none" />

      <div className="absolute top-[-10%] left-[-10%] w-[50%] h-[50%] rounded-full bg-zinc-900/10 blur-[120px] glow-glow pointer-events-none" />
      <div className="absolute bottom-[20%] right-[-10%] w-[40%] h-[40%] rounded-full bg-zinc-900/10 blur-[120px] glow-glow pointer-events-none" />

      {/* Header */}
      <header className="sticky top-0 z-50 glass-card border-b border-zinc-900/60 px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Layers className="w-5 h-5 text-white" />
          <span className="font-mono tracking-widest text-sm font-bold">TESSERA // DEV_PORTAL</span>
        </div>
        <div className="flex items-center gap-6">
          <a href="#customizer" className="text-xs font-mono text-zinc-400 hover:text-white transition-colors">COMPONENTS</a>
          <a href="#hashing" className="text-xs font-mono text-zinc-400 hover:text-white transition-colors">HASH_RING</a>
          <a href="#architecture" className="text-xs font-mono text-zinc-400 hover:text-white transition-colors">SYSTEM_FLOW</a>
          <a href="#docs" className="text-xs font-mono text-zinc-400 hover:text-white transition-colors">DOCUMENTATION</a>
          <div className="h-4 w-px bg-zinc-800" />
          <div className="flex items-center gap-2">
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-white opacity-75"></span>
              <span className="relative inline-flex rounded-full h-2 w-2 bg-white"></span>
            </span>
            <span className="font-mono text-[10px] text-zinc-400">NOMINAL</span>
          </div>
        </div>
      </header>

      {/* Hero Section */}
      <section className="relative px-6 pt-24 pb-20 max-w-7xl mx-auto flex flex-col items-center text-center">
        <div className="inline-flex items-center gap-2 px-3 py-1.5 rounded-full border border-zinc-800 bg-zinc-950/60 mb-6 backdrop-blur">
          <Activity className="w-3.5 h-3.5 text-zinc-400" />
          <span className="font-mono text-[10px] tracking-wider text-zinc-300">MULTI-REGION CONSISTENT HASH SYSTEM</span>
        </div>

        <h1 className="text-5xl md:text-7xl font-bold tracking-tight text-white max-w-4xl leading-tight">
          The Distributed <br />
          <span className="text-zinc-500 font-extrabold">Video-On-Demand Engine</span>
        </h1>

        <p className="mt-6 text-zinc-400 text-sm md:text-base max-w-2xl leading-relaxed font-sans">
          Deploy an end-to-end, high-performance, share-nothing regional transcoding fleet. Ingest files directly, partition dynamically using consistent hashing, and transcode segments concurrently.
        </p>

        <div className="mt-10 flex flex-wrap gap-4 justify-center">
          <a 
            href="#customizer" 
            className="px-6 py-3 bg-white text-black hover:bg-neutral-200 transition-colors font-mono text-xs font-bold rounded flex items-center gap-2"
          >
            GET COMPONENTS <ArrowRight className="w-3.5 h-3.5" />
          </a>
          <a 
            href="#docs" 
            className="px-6 py-3 border border-zinc-800 bg-zinc-950/40 text-zinc-300 hover:text-white hover:border-zinc-700 transition-all font-mono text-xs rounded"
          >
            SEE DOCUMENTATION
          </a>
        </div>
      </section>

      {/* Visual Customizer & Simulator */}
      <section id="customizer" className="border-t border-zinc-900/60 py-24 px-6 max-w-7xl mx-auto">
        <div className="mb-12">
          <h2 className="text-3xl font-bold tracking-tight text-white">Visual Customizer & Simulator</h2>
          <p className="mt-2 text-zinc-400 text-sm">Style the components in real-time. Adjust colors, layout shapes, and test visual component state simulations.</p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-start">
          {/* Controls Panel */}
          <div className="lg:col-span-4 flex flex-col gap-6">
            <div className="glass-card p-6 rounded-lg bg-zinc-950/20 flex flex-col gap-6">
              <div className="flex gap-2 p-1 bg-zinc-950 rounded border border-zinc-900">
                <button 
                  onClick={() => setSelectedComp("uploader")}
                  className={`flex-1 py-2 font-mono text-[10px] font-bold rounded transition-colors ${selectedComp === "uploader" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                >
                  VIDEO_UPLOADER
                </button>
                <button 
                  onClick={() => setSelectedComp("player")}
                  className={`flex-1 py-2 font-mono text-[10px] font-bold rounded transition-colors ${selectedComp === "player" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                >
                  VIDEO_PLAYER
                </button>
                <button 
                  onClick={() => setSelectedComp("tile")}
                  className={`flex-1 py-2 font-mono text-[10px] font-bold rounded transition-colors ${selectedComp === "tile" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                >
                  VIDEO_TILE
                </button>
              </div>

              <div className="h-px bg-zinc-900" />

              {selectedComp === "uploader" ? (
                <div className="flex flex-col gap-4">
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">COMP_TITLE_TEXT</label>
                    <input 
                      type="text"
                      value={uploaderTitle}
                      onChange={(e) => setUploaderTitle(e.target.value)}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                    />
                  </div>
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">BG_COLOR</label>
                    <div className="flex gap-2 items-center">
                      <input 
                        type="color"
                        value={bgColor}
                        onChange={(e) => setBgColor(e.target.value)}
                        className="bg-transparent border-0 cursor-pointer w-8 h-8 rounded"
                      />
                      <span className="font-mono text-xs text-zinc-400">{bgColor}</span>
                    </div>
                  </div>
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">BORDER_COLOR</label>
                    <div className="flex gap-2 items-center">
                      <input 
                        type="color"
                        value={borderColor}
                        onChange={(e) => setBorderColor(e.target.value)}
                        className="bg-transparent border-0 cursor-pointer w-8 h-8 rounded"
                      />
                      <span className="font-mono text-xs text-zinc-400">{borderColor}</span>
                    </div>
                  </div>
                  <div className="grid grid-cols-2 gap-2">
                    <div>
                      <label className="block font-mono text-[10px] text-zinc-500 mb-1">BTN_BG</label>
                      <input 
                        type="color"
                        value={btnBgColor}
                        onChange={(e) => setBtnBgColor(e.target.value)}
                        className="bg-transparent border-0 cursor-pointer w-8 h-8 rounded"
                      />
                    </div>
                    <div>
                      <label className="block font-mono text-[10px] text-zinc-500 mb-1">BTN_TEXT</label>
                      <input 
                        type="color"
                        value={btnTextColor}
                        onChange={(e) => setBtnTextColor(e.target.value)}
                        className="bg-transparent border-0 cursor-pointer w-8 h-8 rounded"
                      />
                    </div>
                  </div>
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">CORNER_RADIUS ({borderRadius}px)</label>
                    <input 
                      type="range"
                      min="0"
                      max="20"
                      value={borderRadius}
                      onChange={(e) => setBorderRadius(Number(e.target.value))}
                      className="w-full accent-white"
                    />
                  </div>
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">CONTAINER_PADDING ({paddingSize}px)</label>
                    <input 
                      type="range"
                      min="12"
                      max="48"
                      value={paddingSize}
                      onChange={(e) => setPaddingSize(Number(e.target.value))}
                      className="w-full accent-white"
                    />
                  </div>
                </div>
              ) : selectedComp === "player" ? (
                <div className="flex flex-col gap-4">
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">CONTROLS_POSITION</label>
                    <div className="flex gap-2 p-1 bg-zinc-950 rounded border border-zinc-900">
                      <button
                        onClick={() => setControlsMode("overlay")}
                        className={`flex-1 py-1.5 font-mono text-[10px] font-bold rounded transition-colors ${controlsMode === "overlay" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                      >
                        OVERLAY
                      </button>
                      <button
                        onClick={() => setControlsMode("below")}
                        className={`flex-1 py-1.5 font-mono text-[10px] font-bold rounded transition-colors ${controlsMode === "below" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                      >
                        BELOW
                      </button>
                    </div>
                  </div>

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">ACCENT_COLOR</label>
                    <div className="flex gap-2 items-center">
                      <input 
                        type="color"
                        value={playerAccent}
                        onChange={(e) => setPlayerAccent(e.target.value)}
                        className="bg-transparent border-0 cursor-pointer w-8 h-8 rounded"
                      />
                      <span className="font-mono text-xs text-zinc-400">{playerAccent}</span>
                    </div>
                  </div>

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">ASPECT_RATIO</label>
                    <select 
                      value={playerAspectRatio}
                      onChange={(e) => setPlayerAspectRatio(e.target.value as any)}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                    >
                      <option value="16/9">Landscape (16:9)</option>
                      <option value="4/3">Standard (4:3)</option>
                      <option value="1/1">Square (1:1)</option>
                    </select>
                  </div>

                  <div className="h-px bg-zinc-900" />

                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="block font-mono text-[10px] text-zinc-500 mb-1">CONTROLS_BG</label>
                      <div className="flex gap-2 items-center">
                        <input 
                          type="color"
                          value={controlsBg}
                          onChange={(e) => setControlsBg(e.target.value)}
                          className="bg-transparent border-0 cursor-pointer w-7 h-7 rounded"
                        />
                        <span className="font-mono text-[10px] text-zinc-400">{controlsBg}</span>
                      </div>
                    </div>
                    <div>
                      <label className="block font-mono text-[10px] text-zinc-500 mb-1">ICON_COLOR</label>
                      <div className="flex gap-2 items-center">
                        <input 
                          type="color"
                          value={controlsIconColor}
                          onChange={(e) => setControlsIconColor(e.target.value)}
                          className="bg-transparent border-0 cursor-pointer w-7 h-7 rounded"
                        />
                        <span className="font-mono text-[10px] text-zinc-400">{controlsIconColor}</span>
                      </div>
                    </div>
                  </div>

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">CONTROLS_BG_OPACITY ({controlsBgOpacity}%)</label>
                    <input 
                      type="range"
                      min="0"
                      max="100"
                      value={controlsBgOpacity}
                      onChange={(e) => setControlsBgOpacity(Number(e.target.value))}
                      className="w-full accent-white"
                    />
                  </div>

                  <div className="h-px bg-zinc-900" />

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">PROGRESS_BAR_COLOR</label>
                    <div className="flex gap-2 items-center">
                      <input 
                        type="color"
                        value={progressBarColor}
                        onChange={(e) => setProgressBarColor(e.target.value)}
                        className="bg-transparent border-0 cursor-pointer w-7 h-7 rounded"
                      />
                      <span className="font-mono text-[10px] text-zinc-400">{progressBarColor}</span>
                    </div>
                  </div>

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">PROGRESS_BAR_HEIGHT ({progressBarHeight}px)</label>
                    <input 
                      type="range"
                      min="2"
                      max="8"
                      value={progressBarHeight}
                      onChange={(e) => setProgressBarHeight(Number(e.target.value))}
                      className="w-full accent-white"
                    />
                  </div>

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">PLAYER_BORDER_RADIUS ({playerBorderRadius}px)</label>
                    <input 
                      type="range"
                      min="0"
                      max="24"
                      value={playerBorderRadius}
                      onChange={(e) => setPlayerBorderRadius(Number(e.target.value))}
                      className="w-full accent-white"
                    />
                  </div>

                  {controlsMode === "overlay" && (
                    <div className="flex items-center justify-between">
                      <label className="font-mono text-[10px] text-zinc-500">SHOW_ON_HOVER</label>
                      <button 
                        onClick={() => setShowControlsOnHover(!showControlsOnHover)}
                        className={`w-9 h-5 rounded-full transition-colors relative ${showControlsOnHover ? "bg-white" : "bg-zinc-800"}`}
                      >
                        <span className={`absolute top-0.5 w-4 h-4 rounded-full transition-all ${showControlsOnHover ? "left-[18px] bg-black" : "left-0.5 bg-zinc-500"}`} />
                      </button>
                    </div>
                  )}
                </div>
              ) : (
                <div className="flex flex-col gap-4">
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">VIDEO_TITLE</label>
                    <input 
                      type="text"
                      value={tileTitle}
                      onChange={(e) => setTileTitle(e.target.value)}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                    />
                  </div>

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">CHANNEL_NAME</label>
                    <input 
                      type="text"
                      value={tileChannel}
                      onChange={(e) => setTileChannel(e.target.value)}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                    />
                  </div>

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">BADGE_TEXT</label>
                    <input 
                      type="text"
                      value={tileBadge}
                      onChange={(e) => setTileBadge(e.target.value)}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                    />
                  </div>

                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">HOVER_PREVIEW_MODE</label>
                    <div className="flex gap-2 p-1 bg-zinc-950 rounded border border-zinc-900">
                      <button
                        onClick={() => setTilePreviewMode("sprite")}
                        className={`flex-1 py-1.5 font-mono text-[10px] font-bold rounded transition-colors ${tilePreviewMode === "sprite" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                      >
                        SPRITE VTT
                      </button>
                      <button
                        onClick={() => setTilePreviewMode("video")}
                        className={`flex-1 py-1.5 font-mono text-[10px] font-bold rounded transition-colors ${tilePreviewMode === "video" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                      >
                        MUTED VIDEO
                      </button>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Visual Component Render & Generated Code panels */}
          <div className="lg:col-span-8 flex flex-col gap-6">
            <div className="glass-card rounded-lg p-8 border border-zinc-900 bg-zinc-950/20 flex flex-col items-center justify-center min-h-[360px]">
              <span className="font-mono text-[9px] text-zinc-500 mb-4 self-start tracking-wider">LIVE_VISUAL_PREVIEW //</span>

              {selectedComp === "uploader" ? (
                <div 
                  style={{
                    backgroundColor: bgColor,
                    borderColor: borderColor,
                    borderRadius: `${borderRadius}px`,
                    padding: `${paddingSize}px`
                  }}
                  className="w-full max-w-md border transition-all duration-300 flex flex-col items-center text-center gap-4 animate-in fade-in"
                >
                  <span className="font-mono text-xs text-zinc-400 font-bold block">{uploaderTitle}</span>
                  
                  {mockUploadProgress === -1 ? (
                    <>
                      <div className="w-full border border-dashed border-zinc-800 rounded-lg p-8 flex flex-col items-center gap-2 bg-black/40">
                        <UploadCloud className="w-8 h-8 text-zinc-500" />
                        <span className="text-[10px] text-zinc-400 font-sans">Drag files here or click to browse</span>
                      </div>
                      <button 
                        onClick={handleMockUpload}
                        style={{
                          backgroundColor: btnBgColor,
                          color: btnTextColor
                        }}
                        className="py-2 px-5 font-mono text-xs font-bold rounded transition-colors"
                      >
                        BROWSE FILE
                      </button>
                    </>
                  ) : (
                    <div className="w-full flex flex-col gap-3">
                      <div className="flex items-center justify-between text-[10px] font-mono text-zinc-400">
                        <span className="truncate max-w-[180px]">{mockFileName}</span>
                        <span>{mockUploadProgress}%</span>
                      </div>
                      <div className="w-full bg-zinc-900 h-1.5 rounded-full overflow-hidden">
                        <div 
                          className="bg-white h-full transition-all duration-300"
                          style={{ width: `${mockUploadProgress}%` }}
                        />
                      </div>
                      <div className="flex items-center justify-between mt-1">
                        <span className="font-mono text-[9px] text-zinc-500 tracking-wider">
                          STATUS: {mockJobState}
                        </span>
                        {mockJobState === "COMPLETED" && (
                          <button 
                            onClick={resetMockUpload}
                            className="text-[9px] font-mono text-white underline hover:text-zinc-300"
                          >
                            RESET
                          </button>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              ) : (
                <div className="w-full max-w-md flex flex-col gap-6 animate-in fade-in duration-200">
                  {/* Video player container */}
                  <div 
                    style={{ borderRadius: `${playerBorderRadius}px` }}
                    className="w-full bg-zinc-950 border border-zinc-900 overflow-hidden flex flex-col transition-all duration-300 relative group"
                  >
                    {/* Aspect-constrained video area */}
                    <div 
                      style={{
                        aspectRatio: playerAspectRatio === "16/9" ? 16/9 : playerAspectRatio === "4/3" ? 4/3 : 1
                      }}
                      className="relative bg-black flex items-center justify-center w-full overflow-hidden"
                    >
                      <video 
                        ref={videoRef}
                        className="w-full h-full object-cover"
                        playsInline
                        onClick={togglePlayback}
                      />
                      
                      {/* Center Play Button Overlay */}
                      {!isPlaying && (
                        <div 
                          onClick={togglePlayback}
                          className="absolute inset-0 bg-black/50 flex items-center justify-center cursor-pointer transition-opacity duration-200"
                        >
                          <button 
                            style={{ backgroundColor: playerAccent, color: controlsBg }}
                            className="rounded-full p-4 hover:scale-110 transition-transform duration-200 shadow-2xl"
                          >
                            <Play className="h-6 w-6 fill-current translate-x-0.5" />
                          </button>
                        </div>
                      )}

                      {/* OVERLAY MODE: Controls inside the video area */}
                      {controlsMode === "overlay" && (
                        <div 
                          className={`absolute bottom-0 left-0 right-0 flex flex-col transition-all duration-300 z-20 ${
                            showControlsOnHover 
                              ? "opacity-0 group-hover:opacity-100 translate-y-1 group-hover:translate-y-0" 
                              : "opacity-100"
                          }`}
                        >
                          {/* Progress bar (above controls) */}
                          <div 
                            className="w-full cursor-pointer group/progress"
                            style={{ height: `${progressBarHeight + 8}px`, padding: '4px 0' }}
                            onClick={(e) => {
                              const video = videoRef.current;
                              if (!video || !duration) return;
                              const rect = e.currentTarget.getBoundingClientRect();
                              const pct = (e.clientX - rect.left) / rect.width;
                              video.currentTime = pct * duration;
                            }}
                          >
                            <div className="relative w-full h-full rounded-full overflow-hidden" style={{ backgroundColor: `${controlsBg}80` }}>
                              {/* Buffered range */}
                              <div 
                                className="absolute top-0 left-0 h-full rounded-full opacity-30"
                                style={{ 
                                  width: `${duration ? ((() => { const v = videoRef.current; if (!v || v.buffered.length === 0) return 0; return (v.buffered.end(v.buffered.length - 1) / duration) * 100; })()) : 0}%`,
                                  backgroundColor: progressBarColor 
                                }}
                              />
                              {/* Current progress */}
                              <div 
                                className="absolute top-0 left-0 h-full rounded-full transition-all duration-150"
                                style={{ 
                                  width: `${duration ? (currentTime / duration) * 100 : 0}%`,
                                  backgroundColor: progressBarColor 
                                }}
                              />
                              {/* Scrubber dot */}
                              <div 
                                className="absolute top-1/2 -translate-y-1/2 w-3 h-3 rounded-full shadow-lg opacity-0 group-hover/progress:opacity-100 transition-opacity"
                                style={{ 
                                  left: `calc(${duration ? (currentTime / duration) * 100 : 0}% - 6px)`,
                                  backgroundColor: progressBarColor 
                                }}
                              />
                            </div>
                          </div>

                          {/* Controls row */}
                          <div 
                            className="px-4 py-2.5 flex items-center justify-between gap-3"
                            style={{ 
                              backgroundColor: (() => {
                                const hex = controlsBg;
                                const r = parseInt(hex.slice(1,3), 16);
                                const g = parseInt(hex.slice(3,5), 16);
                                const b = parseInt(hex.slice(5,7), 16);
                                return `rgba(${r}, ${g}, ${b}, ${controlsBgOpacity / 100})`;
                              })(),
                              backdropFilter: 'blur(12px)',
                              WebkitBackdropFilter: 'blur(12px)'
                            }}
                          >
                            <div className="flex items-center gap-3">
                              <button onClick={togglePlayback} style={{ color: controlsIconColor }} className="hover:opacity-80 transition-opacity">
                                {isPlaying ? <Pause className="w-4 h-4 fill-current" /> : <Play className="w-4 h-4 fill-current" />}
                              </button>
                              
                              <button onClick={toggleMute} style={{ color: controlsIconColor }} className="hover:opacity-80 transition-opacity">
                                {isMuted ? <VolumeX className="w-4 h-4" /> : <Volume2 className="w-4 h-4" />}
                              </button>

                              <span className="font-mono text-[10px]" style={{ color: controlsIconColor, opacity: 0.6 }}>
                                {formatTime(currentTime)} / {formatTime(duration)}
                              </span>
                            </div>
                            
                            <div className="flex items-center gap-2">
                              {/* Quality selector */}
                              <div className="relative">
                                <button 
                                  onClick={() => setShowQualitySelector(prev => !prev)}
                                  className="flex items-center gap-1 px-2 py-1 text-[9px] font-mono rounded transition-opacity hover:opacity-80"
                                  style={{ color: controlsIconColor, border: `1px solid ${controlsIconColor}30` }}
                                >
                                  <SlidersHorizontal className="w-3 h-3" />
                                  {currentHlsLevel === -1 ? "Auto" : hlsLevels[currentHlsLevel] ? hlsLevels[currentHlsLevel].name : "Auto"}
                                </button>
                                
                                {showQualitySelector && (
                                  <div className="absolute bottom-8 right-0 rounded p-1 shadow-2xl w-28 flex flex-col gap-0.5 z-40 border"
                                    style={{ 
                                      backgroundColor: controlsBg, 
                                      borderColor: `${controlsIconColor}20`,
                                    }}
                                  >
                                    <button 
                                      onClick={() => selectQuality(-1)}
                                      className="w-full text-left px-2 py-1.5 text-[9px] font-mono rounded hover:opacity-70 transition-opacity"
                                      style={{ color: currentHlsLevel === -1 ? progressBarColor : controlsIconColor, fontWeight: currentHlsLevel === -1 ? 700 : 400 }}
                                    >
                                      ✦ Auto
                                    </button>
                                    {hlsLevels.map((lvl) => (
                                      <button 
                                        key={lvl.id}
                                        onClick={() => selectQuality(lvl.id)}
                                        className="w-full text-left px-2 py-1.5 text-[9px] font-mono rounded hover:opacity-70 transition-opacity flex flex-col gap-0.5"
                                        style={{ color: currentHlsLevel === lvl.id ? progressBarColor : controlsIconColor, fontWeight: currentHlsLevel === lvl.id ? 700 : 400 }}
                                      >
                                        <span>{lvl.name}</span>
                                        <span style={{ opacity: 0.5, fontSize: '8px' }}>{lvl.bitrate} Kbps</span>
                                      </button>
                                    ))}
                                  </div>
                                )}
                              </div>

                              <button style={{ color: controlsIconColor }} className="hover:opacity-80 transition-opacity">
                                <Maximize className="w-3.5 h-3.5" />
                              </button>
                            </div>
                          </div>
                        </div>
                      )}
                    </div>

                    {/* BELOW MODE: Controls under the video */}
                    {controlsMode === "below" && (
                      <div className="flex flex-col">
                        {/* Progress bar */}
                        <div 
                          className="w-full cursor-pointer group/progress"
                          style={{ height: `${progressBarHeight + 6}px`, padding: '3px 0' }}
                          onClick={(e) => {
                            const video = videoRef.current;
                            if (!video || !duration) return;
                            const rect = e.currentTarget.getBoundingClientRect();
                            const pct = (e.clientX - rect.left) / rect.width;
                            video.currentTime = pct * duration;
                          }}
                        >
                          <div className="relative w-full h-full" style={{ backgroundColor: `${controlsBg}40` }}>
                            <div 
                              className="absolute top-0 left-0 h-full transition-all duration-150"
                              style={{ 
                                width: `${duration ? (currentTime / duration) * 100 : 0}%`,
                                backgroundColor: progressBarColor 
                              }}
                            />
                          </div>
                        </div>

                        {/* Controls row */}
                        <div 
                          className="px-4 py-2.5 flex items-center justify-between gap-3"
                          style={{ 
                            backgroundColor: (() => {
                              const hex = controlsBg;
                              const r = parseInt(hex.slice(1,3), 16);
                              const g = parseInt(hex.slice(3,5), 16);
                              const b = parseInt(hex.slice(5,7), 16);
                              return `rgba(${r}, ${g}, ${b}, ${controlsBgOpacity / 100})`;
                            })()
                          }}
                        >
                          <div className="flex items-center gap-3">
                            <button onClick={togglePlayback} style={{ color: controlsIconColor }} className="hover:opacity-80 transition-opacity">
                              {isPlaying ? <Pause className="w-3.5 h-3.5 fill-current" /> : <Play className="w-3.5 h-3.5 fill-current" />}
                            </button>
                            
                            <button onClick={toggleMute} style={{ color: controlsIconColor }} className="hover:opacity-80 transition-opacity">
                              {isMuted ? <VolumeX className="w-3.5 h-3.5" /> : <Volume2 className="w-3.5 h-3.5" />}
                            </button>

                            <span className="font-mono text-[9px]" style={{ color: controlsIconColor, opacity: 0.6 }}>
                              {formatTime(currentTime)} / {formatTime(duration)}
                            </span>
                          </div>
                          
                          <div className="flex items-center gap-2">
                            <div className="relative">
                              <button 
                                onClick={() => setShowQualitySelector(prev => !prev)}
                                className="flex items-center gap-1 px-2 py-1 text-[9px] font-mono rounded transition-opacity hover:opacity-80"
                                style={{ color: controlsIconColor, border: `1px solid ${controlsIconColor}30` }}
                              >
                                <SlidersHorizontal className="w-3 h-3" />
                                {currentHlsLevel === -1 ? "Auto" : hlsLevels[currentHlsLevel] ? hlsLevels[currentHlsLevel].name : "Auto"}
                              </button>
                              
                              {showQualitySelector && (
                                <div className="absolute bottom-8 right-0 rounded p-1 shadow-2xl w-28 flex flex-col gap-0.5 z-40 border"
                                  style={{ backgroundColor: controlsBg, borderColor: `${controlsIconColor}20` }}
                                >
                                  <button 
                                    onClick={() => selectQuality(-1)}
                                    className="w-full text-left px-2 py-1.5 text-[9px] font-mono rounded hover:opacity-70 transition-opacity"
                                    style={{ color: currentHlsLevel === -1 ? progressBarColor : controlsIconColor, fontWeight: currentHlsLevel === -1 ? 700 : 400 }}
                                  >
                                    ✦ Auto
                                  </button>
                                  {hlsLevels.map((lvl) => (
                                    <button 
                                      key={lvl.id}
                                      onClick={() => selectQuality(lvl.id)}
                                      className="w-full text-left px-2 py-1.5 text-[9px] font-mono rounded hover:opacity-70 transition-opacity flex flex-col gap-0.5"
                                      style={{ color: currentHlsLevel === lvl.id ? progressBarColor : controlsIconColor, fontWeight: currentHlsLevel === lvl.id ? 700 : 400 }}
                                    >
                                      <span>{lvl.name}</span>
                                      <span style={{ opacity: 0.5, fontSize: '8px' }}>{lvl.bitrate} Kbps</span>
                                    </button>
                                  ))}
                                </div>
                              )}
                            </div>

                            <button style={{ color: controlsIconColor }} className="hover:opacity-80 transition-opacity">
                              <Maximize className="w-3.5 h-3.5" />
                            </button>
                          </div>
                        </div>
                      </div>
                    )}
                  </div>

                  {/* Telemetry charts row */}
                  <div className="grid grid-cols-2 gap-4">
                    {/* Buffer occupancy graph */}
                    <div className="border border-zinc-900 bg-zinc-950/40 p-4 rounded-lg flex flex-col gap-2">
                      <div className="flex justify-between items-center text-[9px] font-mono tracking-wider">
                        <span className="text-zinc-500">LIVE BUFFER VALUE</span>
                        <span className="text-white font-bold">{currentBufferSec}s</span>
                      </div>
                      <div className="h-16 w-full relative">
                        <svg className="w-full h-full" viewBox="0 0 145 60" preserveAspectRatio="none">
                          <polyline 
                            fill="none"
                            stroke="#ffffff"
                            strokeWidth="1.5"
                            points={bufferHistory.map((val, idx) => {
                              const x = idx * 5;
                              // Max expected buffer: 15s (map 0-15 to Y 60-0)
                              const y = 60 - Math.min(15, val) * 4;
                              return `${x},${y}`;
                            }).join(" ")}
                          />
                        </svg>
                      </div>
                    </div>

                    {/* Download Bitrate graph */}
                    <div className="border border-zinc-900 bg-zinc-950/40 p-4 rounded-lg flex flex-col gap-2">
                      <div className="flex justify-between items-center text-[9px] font-mono tracking-wider">
                        <span className="text-zinc-500">ACTIVE BANDWIDTH</span>
                        <span className="text-white font-bold">
                          {currentBitrateKbps > 1000 ? `${(currentBitrateKbps / 1000).toFixed(1)} Mbps` : `${currentBitrateKbps} Kbps`}
                        </span>
                      </div>
                      <div className="h-16 w-full relative">
                        <svg className="w-full h-full" viewBox="0 0 145 60" preserveAspectRatio="none">
                          <polyline 
                            fill="none"
                            stroke="#888888"
                            strokeWidth="1.5"
                            points={bitrateHistory.map((val, idx) => {
                              const x = idx * 5;
                              // Max expected bitrate: 6000 Kbps (map 0-6000 to Y 60-0)
                              const y = 60 - Math.min(6000, val) * 0.01;
                              return `${x},${y}`;
                            }).join(" ")}
                          />
                        </svg>
                      </div>
                    </div>
                  </div>
                </div>
              )}
            </div>

            {/* Generated Code Block panel */}
            <div className="glass-card rounded-lg border border-zinc-900 bg-zinc-950/20 overflow-hidden flex flex-col">
              <div className="bg-zinc-950 px-4 py-3 border-b border-zinc-900 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Terminal className="w-4 h-4 text-zinc-400" />
                  <span className="font-mono text-xs text-zinc-400">
                    {selectedComp === "uploader" ? "VideoUploader.tsx" : selectedComp === "player" ? "VideoPlayer.tsx" : "VideoTile.tsx"}
                  </span>
                </div>
                <button 
                  onClick={() => {
                    navigator.clipboard.writeText(generatedJSXCode);
                    setIsCodeCopied(true);
                    setTimeout(() => setIsCodeCopied(false), 2000);
                  }}
                  className="px-3 py-1 bg-zinc-900 hover:bg-zinc-800 text-zinc-300 hover:text-white transition-colors rounded text-[10px] font-mono flex items-center gap-1.5"
                >
                  {isCodeCopied ? (
                    <>
                      <Check className="w-3 h-3 text-green-400" />
                      COPIED
                    </>
                  ) : (
                    <>
                      <Copy className="w-3 h-3" />
                      COPY CODE
                    </>
                  )}
                </button>
              </div>
              <div className="p-6 font-mono text-xs text-zinc-300 overflow-x-auto whitespace-pre leading-relaxed bg-zinc-950/40">
                {generatedJSXCode}
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Consistent Hashing Ring Simulator */}
      <section id="hashing" className="border-t border-zinc-900/60 py-24 px-6 max-w-7xl mx-auto">
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-12 items-center">
          {/* Ring circle */}
          <div className="lg:col-span-6 flex flex-col items-center">
            <div className="w-full max-w-md aspect-square relative flex items-center justify-center">
              <svg className="w-full h-full transform -rotate-90" viewBox="0 0 200 200">
                <circle 
                  cx="100" 
                  cy="100" 
                  r="70" 
                  fill="none" 
                  stroke="#141416" 
                  strokeWidth="12" 
                />
                {/* 8 partition sectors mapped on the ring circle */}
                {[0, 1, 2, 3, 4, 5, 6, 7].map((part) => {
                  const strokeDasharray = `${2 * Math.PI * 70 / 8 - 3} ${2 * Math.PI * 70 / 8 + 3}`;
                  const strokeDashoffset = `${part * (2 * Math.PI * 70 / 8)}`;
                  const isHovered = activePartition === part;
                  const owner = getPartitionOwner(part);
                  
                  // Shade codes for active partitions based on owner index
                  const colors = ["#ffffff", "#cccccc", "#888888", "#555555", "#333333"];
                  const ownerIndex = nodes.indexOf(owner);
                  const strokeColor = ownerIndex !== -1 ? colors[ownerIndex % colors.length] : "#1a1a1a";

                  return (
                    <circle 
                      key={part}
                      cx="100" 
                      cy="100" 
                      r="70" 
                      fill="none" 
                      stroke={strokeColor}
                      strokeWidth={isHovered ? "18" : "12"}
                      strokeDasharray={strokeDasharray}
                      strokeDashoffset={strokeDashoffset}
                      className="cursor-pointer transition-all duration-300"
                      onMouseEnter={() => setActivePartition(part)}
                      onMouseLeave={() => setActivePartition(null)}
                    />
                  );
                })}

                {/* Node coordinates mapped around circle boundary */}
                {nodes.map((node) => {
                  const angle = getNodeAngle(node);
                  const radians = (angle * Math.PI) / 180;
                  const x = 100 + 70 * Math.cos(radians);
                  const y = 100 + 70 * Math.sin(radians);
                  return (
                    <circle 
                      key={node}
                      cx={x} 
                      cy={y} 
                      r="5" 
                      fill="#ffffff" 
                      stroke="#000000"
                      strokeWidth="1.5"
                    />
                  );
                })}
              </svg>

              {/* Ring core description label */}
              <div className="absolute inset-0 flex flex-col items-center justify-center text-center p-8 pointer-events-none">
                {activePartition !== null ? (
                  <>
                    <span className="font-mono text-[10px] text-zinc-500 tracking-widest">PARTITION_{activePartition}</span>
                    <span className="text-lg font-bold text-white mt-1">SLOT OWNER</span>
                    <span className="font-mono text-xs text-zinc-300 mt-1 truncate max-w-[200px]">
                      {getPartitionOwner(activePartition)}
                    </span>
                    <span className="font-mono text-[9px] text-zinc-500 mt-0.5">ANGLE: {activePartition * 45}°</span>
                  </>
                ) : (
                  <>
                    <Network className="w-8 h-8 text-zinc-500 mb-1" />
                    <span className="font-mono text-[10px] text-zinc-400">8_PARTITIONS_RING</span>
                    <span className="text-[9px] text-zinc-500 font-mono mt-1">HOVER SECTOR TO INSPECT</span>
                  </>
                )}
              </div>
            </div>
          </div>

          {/* Nodes panel */}
          <div className="lg:col-span-6 flex flex-col gap-6">
            <div>
              <h2 className="text-3xl font-bold tracking-tight text-white">Consistent Hash Ring</h2>
              <p className="mt-2 text-zinc-400 text-sm">
                Active coordinators lock and lease partition slices dynamically. We calculate coordinator slot placements via deterministic FNV-1a hashing on [0°, 360°] angles.
              </p>
            </div>

            {/* List of active nodes */}
            <div className="glass-card p-6 rounded-lg bg-zinc-950/20">
              <span className="block font-mono text-[10px] text-zinc-500 tracking-wider mb-3">ACTIVE_COORDINATORS</span>
              <div className="flex flex-col gap-2 max-h-[180px] overflow-y-auto pr-2">
                {nodes.map((node) => {
                  const angle = getNodeAngle(node);
                  const nodePartitions = [0, 1, 2, 3, 4, 5, 6, 7].filter(p => getPartitionOwner(p) === node);
                  return (
                    <div key={node} className="flex items-center justify-between border border-zinc-900 bg-zinc-950 p-3 rounded">
                      <div className="flex items-center gap-2.5 min-w-0">
                        <Server className="w-4 h-4 text-white shrink-0" />
                        <div className="flex flex-col min-w-0">
                          <span className="font-mono text-xs text-zinc-300 truncate">{node}</span>
                          <span className="font-mono text-[9px] text-zinc-500">Hash Angle: {angle}°</span>
                        </div>
                        <div className="flex gap-1 flex-wrap shrink-0">
                          {nodePartitions.map(p => (
                            <span key={p} className="px-1.5 py-0.5 bg-zinc-900 border border-zinc-800 rounded font-mono text-[9px] text-zinc-400">
                              P{p}
                            </span>
                          ))}
                        </div>
                      </div>
                      <button 
                        onClick={() => removeNode(node)}
                        className="p-1 hover:bg-zinc-900 rounded text-zinc-500 hover:text-white transition-colors"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  );
                })}
              </div>

              {/* Add node controller */}
              <div className="flex gap-2 mt-4">
                <input 
                  type="text" 
                  placeholder="coordinator-node-name" 
                  value={newNodeName}
                  onChange={(e) => setNewNodeName(e.target.value)}
                  className="flex-1 bg-zinc-950 border border-zinc-900 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-600 transition-colors"
                />
                <button 
                  onClick={addNode}
                  className="px-4 py-2 bg-white text-black hover:bg-neutral-200 transition-colors font-mono text-xs font-bold rounded flex items-center gap-1"
                >
                  <Plus className="w-3.5 h-3.5" /> ADD
                </button>
              </div>
            </div>

            {/* Rebalance console logs */}
            <div className="border border-zinc-900 bg-zinc-950 rounded-lg p-4 font-mono text-[10px] text-zinc-400 h-[120px] overflow-y-auto">
              <span className="block text-zinc-600 mb-1.5">// CONSISTENT_RING_REBALANCE_STREAM</span>
              {rebalanceLog.map((log, idx) => (
                <div key={idx} className="truncate text-zinc-400">
                  <span className="text-zinc-600 mr-2">&gt;</span>{log}
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* Visual Ingestion Pipeline Map */}
      <section id="architecture" className="border-t border-zinc-900/60 py-24 px-6 max-w-7xl mx-auto">
        <div className="mb-12">
          <h2 className="text-3xl font-bold tracking-tight text-white">Dynamic System Pipeline</h2>
          <p className="mt-2 text-zinc-400 text-sm">Visualizing the parallel, region-isolated flow of video chunk transcoding.</p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-stretch">
          {/* SVG Ingest Route Visualizer */}
          <div className="lg:col-span-8 glass-card p-8 rounded-lg bg-zinc-950/20 flex flex-col justify-between">
            <div className="flex flex-wrap gap-4 items-center justify-around py-12 relative">
              <div className="absolute top-1/2 left-[10%] right-[10%] h-px border-t border-dashed border-zinc-900 -translate-y-1/2 z-0" />

              {[
                { id: "ingest", label: "INGEST", icon: UploadCloud },
                { id: "coordinator", label: "ETCD_LEASES", icon: Server },
                { id: "messagebus", label: "NATS_QUEUE", icon: Network },
                { id: "workers", label: "FFMPEG_FLEETS", icon: Cpu },
                { id: "compilation", label: "S3_CRR_SYNC", icon: Database }
              ].map((stage) => {
                const IconComponent = stage.icon;
                const isActive = activeArchStage === stage.id;
                return (
                  <button 
                    key={stage.id}
                    onClick={() => setActiveArchStage(stage.id)}
                    className={`relative z-10 p-5 rounded-lg border flex flex-col items-center gap-3 transition-all duration-300 cursor-pointer ${isActive ? "bg-white border-white text-black scale-105" : "bg-zinc-950 border-zinc-900 text-zinc-400 hover:border-zinc-700 hover:text-white"}`}
                  >
                    <IconComponent className="w-6 h-6" />
                    <span className="font-mono text-[9px] tracking-widest font-bold">{stage.label}</span>
                  </button>
                );
              })}
            </div>

            <div className="h-px bg-zinc-900 my-6" />

            <div className="flex items-center gap-3">
              <Globe className="w-4 h-4 text-zinc-500" />
              <span className="font-mono text-[10px] text-zinc-500">DATA_GRAVITY_CONSTRAINT_ENABLED</span>
            </div>
          </div>

          {/* Explanation panel */}
          <div className="lg:col-span-4 flex flex-col">
            <div className="glass-card rounded-lg p-6 bg-zinc-950/20 border border-zinc-900 flex-1 flex flex-col justify-between">
              <div>
                <span className="font-mono text-[10px] text-zinc-500 tracking-wider block mb-2">STAGE_EXPLANATION //</span>
                <h3 className="text-lg font-bold text-white">{stageExplanations[activeArchStage].title}</h3>
                <p className="mt-4 text-zinc-400 text-xs leading-relaxed">
                  {stageExplanations[activeArchStage].desc}
                </p>
              </div>

              <div className="mt-8 border border-zinc-900 bg-zinc-950 p-4 rounded font-mono text-[10px]">
                <span className="block text-zinc-500 mb-1">METRICS / LIMITS:</span>
                <span className="text-white">{stageExplanations[activeArchStage].metrics}</span>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Comprehensive System Manuals Explorer */}
      <section id="docs" className="border-t border-zinc-900/60 py-24 px-6 max-w-7xl mx-auto">
        <div className="mb-12">
          <h2 className="text-3xl font-bold tracking-tight text-white">System Documentation</h2>
          <p className="mt-2 text-zinc-400 text-sm">Read the full master production reference guides regarding regional setup, deployment specifications, and SDK integration APIs.</p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-start">
          {/* Selector Tabs */}
          <div className="lg:col-span-3 flex flex-col gap-2">
            {[
              { id: "quickstart", label: "1. Developer Integration", icon: BookOpen },
              { id: "deployment", label: "2. Multi-Region Deployment", icon: Globe },
              { id: "master", label: "3. SRE Master Reference", icon: Settings }
            ].map((tab) => {
              const IconComp = tab.icon;
              return (
                <button 
                  key={tab.id}
                  onClick={() => setDocTab(tab.id as any)}
                  className={`flex items-center gap-3 px-4 py-3 rounded text-left font-mono text-xs transition-colors ${docTab === tab.id ? "bg-zinc-900 text-white font-bold" : "text-zinc-400 hover:text-white hover:bg-zinc-950/60"}`}
                >
                  <IconComp className="w-4 h-4 shrink-0" />
                  {tab.label}
                </button>
              );
            })}
          </div>

          {/* Detailed Document Content Panels */}
          <div className="lg:col-span-9">
            <div className="glass-card rounded-lg border border-zinc-900 p-8 bg-zinc-950/20 font-sans leading-relaxed text-zinc-300 text-sm max-h-[600px] overflow-y-auto flex flex-col gap-6">
              {docTab === "quickstart" && (
                <>
                  <h3 className="text-2xl font-bold text-white border-b border-zinc-900 pb-3 font-mono">DEVELOPER_INTEGRATION_GUIDE</h3>
                  <p className="text-zinc-400 italic">This guide details the ingestion API session configuration, Direct S3 Chunk Ingestion, and real-time SSE progress tracking.</p>
                  
                  <div className="flex flex-col gap-4">
                    <h4 className="text-sm font-bold text-white font-mono border-l-2 border-white pl-2">1. Ingestion Session Creation</h4>
                    <p>Before uploading, clients negotiate a session token. Pass file size and file name details to avoid unauthorized storage floods:</p>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`POST /api/jobs/upload-session
Headers: Content-Type: application/json
Payload:
{
  "file_size_bytes": 104857600, // 100MB
  "file_name": "marketing_reel.mp4",
  "content_type": "video/mp4"
}

Response (Session Token & Part Info):
{
  "job_id": "us-east-1:7ff8b548-c8ee-449e-b7d1-c27633f81e3a",
  "session_token": "eyJhbGciOiJIUzI1NiIsIn...", // Secure JWT Token
  "part_size": 52428800,                         // Part size in bytes (50MB)
  "total_parts": 2                               // Client must split file into 2 parts
}`}
                    </pre>
                  </div>

                  <div className="flex flex-col gap-4">
                    <h4 className="text-sm font-bold text-white font-mono border-l-2 border-white pl-2">2. Direct S3 Chunk Upload</h4>
                    <p>Clients request presigned URLs by presenting the JWT session token, and upload chunks directly using standard PUT requests:</p>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`POST /api/jobs/{job_id}/urls?start=1&count=2
Headers: Authorization: Bearer <session_token>

Response:
{
  "part_numbers": [1, 2],
  "urls": [
    "http://minio-ip:9000/transcoder-bucket/jobs/.../raw/source.mp4?partNumber=1&uploadId=...",
    "http://minio-ip:9000/transcoder-bucket/jobs/.../raw/source.mp4?partNumber=2&uploadId=..."
  ]
}`}
                    </pre>
                  </div>

                  <div className="flex flex-col gap-4">
                    <h4 className="text-sm font-bold text-white font-mono border-l-2 border-white pl-2">3. Client JavaScript Integration Code</h4>
                    <p>Full implementation workflow for direct uploads and progress listening via Server-Sent Events (SSE):</p>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`async function uploadAndTranscode(file, gatewayUrl) {
  // Step 1: Initialize Session
  const sessionRes = await fetch(\`\${gatewayUrl}/api/jobs/upload-session\`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ file_size_bytes: file.size, file_name: file.name })
  });
  const { job_id, session_token, part_size, total_parts } = await sessionRes.json();

  // Step 2: Fetch Presigned URLs
  const urlRes = await fetch(\`\${gatewayUrl}/api/jobs/\${job_id}/urls?start=1&count=\${total_parts}\`, {
    method: "POST",
    headers: { "Authorization": \`Bearer \${session_token}\` }
  });
  const { urls } = await urlRes.json();

  // Step 3: Upload chunks in parallel
  const completedParts = [];
  const uploadPromises = [];
  for (let i = 0; i < total_parts; i++) {
    const start = i * part_size;
    const end = Math.min(start + part_size, file.size);
    const chunk = file.slice(start, end);

    const p = fetch(urls[i], { method: "PUT", body: chunk })
      .then(res => {
        const etag = res.headers.get("ETag");
        completedParts.push({ part_number: i + 1, etag });
      });
    uploadPromises.push(p);
  }
  await Promise.all(uploadPromises);

  // Step 4: Complete Session
  await fetch(\`\${gatewayUrl}/api/jobs/\${job_id}/complete\`, {
    method: "POST",
    headers: {
      "Authorization": \`Bearer \${session_token}\`,
      "Content-Type": "application/json"
    },
    body: JSON.stringify({ parts: completedParts })
  });

  // Step 5: Listen to SSE progress
  const source = new EventSource(\`\${gatewayUrl}/progress/\${job_id}\`);
  source.onmessage = (e) => {
    const update = JSON.parse(e.data);
    console.log(\`Transcode progress: \${update.pct}%\`);
    if (update.phase === "COMPLETED") {
      source.close();
      console.log("Transcoding finished successfully!");
    }
  };
}`}
                    </pre>
                  </div>
                </>
              )}

              {docTab === "deployment" && (
                <>
                  <h3 className="text-2xl font-bold text-white border-b border-zinc-900 pb-3 font-mono">MULTI_REGION_DEPLOYMENT_GUIDE</h3>
                  <p className="text-zinc-400 italic">This guide details network routing, configurations, bucket replication policies, and disaster failovers.</p>

                  <div className="flex flex-col gap-4">
                    <h4 className="text-sm font-bold text-white font-mono border-l-2 border-white pl-2">1. Geo-DNS & Storage Ingest routing</h4>
                    <ul className="list-disc pl-5 flex flex-col gap-2">
                      <li>Use Geolocation DNS routing (e.g. AWS Route 53 Geolocation) or Anycast IPs to resolve API gateway requests to the closest regional cluster.</li>
                      <li>Presigned URLs point to region-local S3 buckets, keeping heavy video chunk uploads local to avoid WAN packet routing latency.</li>
                    </ul>
                  </div>

                  <div className="flex flex-col gap-4">
                    <h4 className="text-sm font-bold text-white font-mono border-l-2 border-white pl-2">2. AWS S3 Cross-Region Replication (CRR) Policy</h4>
                    <p>Apply this configuration to S3 source buckets to replicate lightweight master playlist files, while explicitly excluding heavy media `.ts` segment chunks:</p>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`{
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
            { "Key": "replicate", "Value": "true" }
          ]
        }
      },
      "Destination": {
        "Bucket": "arn:aws:s3:::apple-transcoder-eu-west",
        "StorageClass": "STANDARD"
      }
    }
  ]
}`}
                    </pre>
                  </div>

                  <div className="flex flex-col gap-4">
                    <h4 className="text-sm font-bold text-white font-mono border-l-2 border-white pl-2">3. SRE Disaster Failover Workflow</h4>
                    <ul className="list-disc pl-5 flex flex-col gap-2">
                      <li>Update Geo-DNS routing configs to divert traffic away from the affected region.</li>
                      <li>Active clients will receive connections from the healthy region, and can instantly upload files from scratch.</li>
                      <li>Replicated manifest entries are accessible from the destination bucket, guaranteeing playback continuity for all completed videos.</li>
                    </ul>
                  </div>
                </>
              )}

              {docTab === "master" && (
                <>
                  <h3 className="text-2xl font-bold text-white border-b border-zinc-900 pb-3 font-mono">MASTER_PRODUCTION_REFERENCE_MANUAL</h3>
                  <p className="text-zinc-400 italic">Core operational configurations, consensus models, consistent hash ring topologies, and pluggable bus interfaces.</p>

                  <div className="flex flex-col gap-4">
                    <h4 className="text-sm font-bold text-white font-mono border-l-2 border-white pl-2">1. Unified Config Schema</h4>
                    <p>Production cluster configuration settings detailing etcd lease times, NATS stream division counts, and worker thread quotas:</p>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`# config.yaml
role: "gateway"
region: "us-east-1"
message_bus_provider: "nats"

redis:
  addrs: ["redis-us-east.internal:6379"]
  password: "prod-redis-password"
  pool_size: 100

nats:
  urls: ["nats://nats-us-east.internal:4222"]

etcd:
  endpoints: ["etcd-us-east.internal:2379"]

object_store:
  endpoint: "s3.us-east-1.amazonaws.com"
  bucket: "apple-transcoder-us-east"
  region: "us-east-1"
  use_ssl: true

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
  concurrent_tasks: 8
  hw_accel: "nvenc" # "nvenc" | "videotoolbox" | "none"`}
                    </pre>
                  </div>

                  <div className="flex flex-col gap-4">
                    <h4 className="text-sm font-bold text-white font-mono border-l-2 border-white pl-2">2. Pluggable Infrastructure Drivers</h4>
                    <p>Our Go engine utilizes abstract interfaces to support swapping subsystems. Developers can configure AWS SQS and DynamoDB by mapping configuration rules:</p>
                    <ul className="list-disc pl-5 flex flex-col gap-2">
                      <li><strong>MessageBus</strong>: Swap NATS for SQS seamlessly under high load networks.</li>
                      <li><strong>StateStore</strong>: Swap Redis for DynamoDB to handle extreme state caches without database instance maintenance.</li>
                    </ul>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </section>

      {/* Footer */}
      <footer className="border-t border-zinc-900/60 py-12 px-6 bg-zinc-950/20 text-center font-mono text-[10px] text-zinc-600">
        <div className="max-w-7xl mx-auto flex flex-col md:flex-row justify-between items-center gap-4">
          <span>&copy; 2026 TESSERA. ALL RIGHTS RESERVED.</span>
          <div className="flex gap-4">
            <span className="text-zinc-500">VERSION 1.3.0-PREMIUM-PREVIEW</span>
            <span className="text-zinc-500">|</span>
            <span className="text-zinc-500">DEPLOYMENT: MULTI_REGION_ACTIVE</span>
          </div>
        </div>
      </footer>
    </main>
  );
}
