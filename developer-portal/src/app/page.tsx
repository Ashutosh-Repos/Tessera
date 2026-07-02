"use client";

import React, { useState, useEffect } from "react";
import { 
  Server, 
  UploadCloud, 
  Play, 
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
  RefreshCw,
  Globe
} from "lucide-react";

export default function Home() {
  // --- Consistent Hashing Playground State ---
  const [nodes, setNodes] = useState<string[]>([
    "coordinator-node-us-east-1",
    "coordinator-node-us-east-2",
    "coordinator-node-eu-west-1"
  ]);
  const [newNodeName, setNewNodeName] = useState<string>("");
  const [activePartition, setActivePartition] = useState<number | null>(null);
  const [rebalanceLog, setRebalanceLog] = useState<string[]>([
    "Ring initialized with 4 partition slots.",
    "Mapped coordinator-node-us-east-1 to partitions [0].",
    "Mapped coordinator-node-us-east-2 to partitions [1, 3].",
    "Mapped coordinator-node-eu-west-1 to partitions [2]."
  ]);

  // Add node to hash ring
  const addNode = () => {
    const name = newNodeName.trim() || `coordinator-node-${Math.random().toString(36).substr(2, 5)}`;
    if (nodes.includes(name)) return;
    const updated = [...nodes, name];
    setNodes(updated);
    setNewNodeName("");
    setRebalanceLog(prev => [
      `Node '${name}' registered lease in etcd.`,
      `Rebalancing 4 partition slots across ${updated.length} active nodes...`,
      ...prev
    ]);
  };

  // Remove node from hash ring
  const removeNode = (target: string) => {
    if (nodes.length <= 1) {
      setRebalanceLog(prev => ["Cannot remove last node. Cluster needs at least 1 active coordinator.", ...prev]);
      return;
    }
    const updated = nodes.filter(n => n !== target);
    setNodes(updated);
    setRebalanceLog(prev => [
      `Node '${target}' lease expired/fenced.`,
      `Rebalancing 4 partition slots across ${updated.length} active nodes...`,
      ...prev
    ]);
  };

  // Map partitions dynamically based on simple index hashing
  const getPartitionOwner = (partIdx: number) => {
    if (nodes.length === 0) return "none";
    // Consistent hashing simulator mapping
    const hash = (partIdx * 7) % nodes.length;
    return nodes[hash];
  };

  // --- React Component Customizer State ---
  const [selectedComponent, setSelectedComponent] = useState<"uploader" | "player">("uploader");
  const [gatewayUrl, setGatewayUrl] = useState<string>("http://localhost:8080");
  const [maxSizeGb, setMaxSizeGb] = useState<number>(5);
  const [hwAccel, setHwAccel] = useState<string>("none");
  const [themeStyle, setThemeStyle] = useState<string>("stark-black-white");
  const [isCopied, setIsCopied] = useState<boolean>(false);

  // Copy code utility
  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setIsCopied(true);
    setTimeout(() => setIsCopied(false), 2000);
  };

  // Code generator templates
  const generatedUploaderCode = `import { VideoUploader } from '@transcoder/ui-sdk';

function App() {
  return (
    <div className="max-w-2xl mx-auto p-8">
      <VideoUploader 
        gatewayUrl="${gatewayUrl}"
        maxSizeGb={${maxSizeGb}}
        theme="${themeStyle}"
        classNames={{
          container: "border border-zinc-800 bg-zinc-950 p-6 rounded-lg",
          dropZone: "border-2 border-dashed border-zinc-700 hover:border-white transition-all",
          uploadBtn: "bg-white text-black hover:bg-neutral-200 transition-colors font-mono"
        }}
        onUploadComplete={(job) => {
          console.log("Ingestion started:", job.job_id);
        }}
      />
    </div>
  );
}`;

  const generatedPlayerCode = `import { VideoPlayer } from '@transcoder/ui-sdk';

function App() {
  return (
    <div className="max-w-4xl mx-auto aspect-video">
      <VideoPlayer 
        src="${gatewayUrl}/playback/master.m3u8"
        hwAccel="${hwAccel}"
        autoPlay={false}
        theme="${themeStyle}"
        onQualityChange={(level) => {
          console.log("Quality updated to:", level.height);
        }}
      />
    </div>
  );
}`;

  // --- Architecture Explorer Hover State ---
  const [activeArchStage, setActiveArchStage] = useState<string>("ingest");
  const stageExplanations: Record<string, { title: string; desc: string; metrics: string }> = {
    ingest: {
      title: "1. Client Direct Storage Ingestion",
      desc: "Gateway generates JWT-signed AWS S3 presigned URLs. The client uploads chunks directly to S3-compatible storage, enforcing local Data Gravity and eliminating network ingress bottleneck.",
      metrics: "Ingest Limit: 50GB/job | WAN hop latency: 0ms"
    },
    coordinator: {
      title: "2. Consistent Hash Ring Slicing",
      desc: "etcd tracks registered coordinator leases. Upload events are dispatched via partition keys. The lease-owning coordinator takes an etcd lock, relocates faststart index, and slices the MP4 into 5s sub-segments.",
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

  // --- Documentation Search/Toggles ---
  const [docTab, setDocTab] = useState<"quickstart" | "deployment" | "isolation">("quickstart");

  return (
    <main className="relative min-h-screen bg-black text-foreground overflow-x-hidden">
      {/* Background Grid Pattern */}
      <div className="absolute inset-0 grid-bg opacity-40 pointer-events-none" />

      {/* Decorative Glow Elements */}
      <div className="absolute top-[-10%] left-[-10%] w-[50%] h-[50%] rounded-full bg-zinc-900/10 blur-[120px] glow-glow pointer-events-none" />
      <div className="absolute bottom-[20%] right-[-10%] w-[40%] h-[40%] rounded-full bg-zinc-900/10 blur-[120px] glow-glow pointer-events-none" />

      {/* Header */}
      <header className="sticky top-0 z-50 glass-card border-b border-zinc-900/60 px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Layers className="w-5 h-5 text-white" />
          <span className="font-mono tracking-widest text-sm font-bold">VOD_CLUSTER // DEV_PORTAL</span>
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

        {/* Feature stats micro-bento */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mt-20 w-full">
          {[
            { label: "WAN DATA LEAK", value: "0%" },
            { label: "CRR SYNC TIME", value: "<2s" },
            { label: "COMPILER Presets", value: "1080p, 720p, 480p" },
            { label: "MAX INGEST LOAD", value: "50GB/Job" }
          ].map((item, idx) => (
            <div key={idx} className="glass-card p-5 rounded-lg text-left border border-zinc-900 bg-zinc-950/20">
              <span className="block font-mono text-[10px] text-zinc-500 tracking-wider mb-1">{item.label}</span>
              <span className="text-xl font-bold text-white font-mono">{item.value}</span>
            </div>
          ))}
        </div>
      </section>

      {/* Interactive Code customizer playground */}
      <section id="customizer" className="border-t border-zinc-900/60 py-24 px-6 max-w-7xl mx-auto">
        <div className="mb-12">
          <h2 className="text-3xl font-bold tracking-tight text-white">Interactive Customizer</h2>
          <p className="mt-2 text-zinc-400 text-sm">Configure components dynamically and copy integration-ready React code snippets.</p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-12 gap-8">
          {/* Left panel options */}
          <div className="lg:col-span-4 flex flex-col gap-6">
            <div className="glass-card p-6 rounded-lg bg-zinc-950/20 flex flex-col gap-6">
              <div className="flex gap-2 p-1 bg-zinc-950 rounded border border-zinc-900">
                <button 
                  onClick={() => setSelectedComponent("uploader")}
                  className={`flex-1 py-2 font-mono text-xs font-bold rounded transition-colors ${selectedComponent === "uploader" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                >
                  VIDEO_UPLOADER
                </button>
                <button 
                  onClick={() => setSelectedComponent("player")}
                  className={`flex-1 py-2 font-mono text-xs font-bold rounded transition-colors ${selectedComponent === "player" ? "bg-white text-black" : "text-zinc-400 hover:text-white"}`}
                >
                  VIDEO_PLAYER
                </button>
              </div>

              <div className="h-px bg-zinc-900" />

              {/* Shared parameters */}
              <div className="flex flex-col gap-4">
                <div>
                  <label className="block font-mono text-[10px] text-zinc-500 mb-1">GATEWAY_ENDPOINT</label>
                  <input 
                    type="text" 
                    value={gatewayUrl} 
                    onChange={(e) => setGatewayUrl(e.target.value)}
                    className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                  />
                </div>

                {selectedComponent === "uploader" ? (
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">MAX_UPLOAD_SIZE (GB)</label>
                    <select 
                      value={maxSizeGb} 
                      onChange={(e) => setMaxSizeGb(Number(e.target.value))}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                    >
                      <option value={1}>1 GB</option>
                      <option value={5}>5 GB</option>
                      <option value={15}>15 GB</option>
                      <option value={50}>50 GB</option>
                    </select>
                  </div>
                ) : (
                  <div>
                    <label className="block font-mono text-[10px] text-zinc-500 mb-1">HARDWARE_ACCELERATION</label>
                    <select 
                      value={hwAccel} 
                      onChange={(e) => setHwAccel(e.target.value)}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                    >
                      <option value="none">None (Software libx264)</option>
                      <option value="videotoolbox">Apple Videotoolbox</option>
                      <option value="nvenc">NVIDIA NVENC</option>
                      <option value="vaapi">VAAPI</option>
                    </select>
                  </div>
                )}

                <div>
                  <label className="block font-mono text-[10px] text-zinc-500 mb-1">THEME_PROFILE</label>
                  <select 
                    value={themeStyle} 
                    onChange={(e) => setThemeStyle(e.target.value)}
                    className="w-full bg-zinc-950 border border-zinc-800 rounded px-3 py-2 text-xs font-mono text-white focus:outline-none focus:border-zinc-500 transition-colors"
                  >
                    <option value="stark-black-white">Stark Minimalist (B&W)</option>
                    <option value="glassmorphic">Glassmorphic Translucent</option>
                    <option value="bordered-zinc">Thin Slate Border</option>
                  </select>
                </div>
              </div>
            </div>
          </div>

          {/* Right code panel */}
          <div className="lg:col-span-8 flex flex-col">
            <div className="glass-card flex-1 rounded-lg border border-zinc-900 bg-zinc-950/20 overflow-hidden flex flex-col">
              <div className="bg-zinc-950 px-4 py-3 border-b border-zinc-900 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Terminal className="w-4 h-4 text-zinc-400" />
                  <span className="font-mono text-xs text-zinc-400">
                    {selectedComponent === "uploader" ? "VideoUploader.tsx" : "VideoPlayer.tsx"}
                  </span>
                </div>
                <button 
                  onClick={() => copyToClipboard(selectedComponent === "uploader" ? generatedUploaderCode : generatedPlayerCode)}
                  className="px-3 py-1 bg-zinc-900 hover:bg-zinc-800 text-zinc-300 hover:text-white transition-colors rounded text-[10px] font-mono flex items-center gap-1.5"
                >
                  {isCopied ? (
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
              <div className="p-6 font-mono text-xs text-zinc-300 overflow-x-auto whitespace-pre leading-relaxed bg-zinc-950/40 flex-1">
                {selectedComponent === "uploader" ? generatedUploaderCode : generatedPlayerCode}
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Consistent Hashing Ring Simulator */}
      <section id="hashing" className="border-t border-zinc-900/60 py-24 px-6 max-w-7xl mx-auto">
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-12 items-center">
          {/* Left panel text and ring visualization */}
          <div className="lg:col-span-6 flex flex-col items-center">
            <div className="w-full max-w-md aspect-square relative flex items-center justify-center">
              {/* Ring circle */}
              <svg className="w-full h-full transform -rotate-90" viewBox="0 0 200 200">
                <circle 
                  cx="100" 
                  cy="100" 
                  r="70" 
                  fill="none" 
                  stroke="#141416" 
                  strokeWidth="12" 
                />
                {/* 4 partition slices mapped on the ring circle */}
                {[0, 1, 2, 3].map((part) => {
                  const strokeDasharray = `${2 * Math.PI * 70 / 4 - 3} ${2 * Math.PI * 70 / 4 + 3}`;
                  const strokeDashoffset = `${part * (2 * Math.PI * 70 / 4)}`;
                  const isHovered = activePartition === part;
                  const owner = getPartitionOwner(part);
                  
                  // Color codes for active partitions based on owner index
                  const colors = ["#ffffff", "#cccccc", "#888888", "#555555"];
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
                  </>
                ) : (
                  <>
                    <Network className="w-8 h-8 text-zinc-500 mb-1" />
                    <span className="font-mono text-[10px] text-zinc-400">1024_PARTITIONS_RING</span>
                    <span className="text-[9px] text-zinc-500 font-mono mt-1">HOVER SECTOR TO INSPECT</span>
                  </>
                )}
              </div>
            </div>
          </div>

          {/* Right panel nodes dashboard list & logs */}
          <div className="lg:col-span-6 flex flex-col gap-6">
            <div>
              <h2 className="text-3xl font-bold tracking-tight text-white">Consistent Hash Ring</h2>
              <p className="mt-2 text-zinc-400 text-sm">
                Active coordinators lock and lease partition slices dynamically. When nodes scale or drop, partition segments automatically rebalance without active session interruption.
              </p>
            </div>

            {/* List of active nodes */}
            <div className="glass-card p-6 rounded-lg bg-zinc-950/20">
              <span className="block font-mono text-[10px] text-zinc-500 tracking-wider mb-3">ACTIVE_COORDINATORS</span>
              <div className="flex flex-col gap-2 max-h-[180px] overflow-y-auto pr-2">
                {nodes.map((node, index) => {
                  const nodePartitions = [0, 1, 2, 3].filter(p => getPartitionOwner(p) === node);
                  return (
                    <div key={node} className="flex items-center justify-between border border-zinc-900 bg-zinc-950 p-3 rounded">
                      <div className="flex items-center gap-2.5 min-w-0">
                        <Server className="w-4 h-4 text-white flex-shrink-0" />
                        <span className="font-mono text-xs text-zinc-300 truncate">{node}</span>
                        <div className="flex gap-1 flex-shrink-0">
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
              {/* Connector Wires (Dotted lines in backdrop) */}
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

      {/* Fully styled documentation viewer */}
      <section id="docs" className="border-t border-zinc-900/60 py-24 px-6 max-w-7xl mx-auto">
        <div className="mb-12">
          <h2 className="text-3xl font-bold tracking-tight text-white">System Documentation</h2>
          <p className="mt-2 text-zinc-400 text-sm">Read guides regarding setups, SDK integration details, and regional configuration files.</p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-12 gap-8 items-start">
          {/* Selector Tabs */}
          <div className="lg:col-span-3 flex flex-col gap-2">
            {[
              { id: "quickstart", label: "1. Developer Integration", icon: BookOpen },
              { id: "deployment", label: "2. Multi-Region Deployment", icon: Globe },
              { id: "isolation", label: "3. Regional Config Schema", icon: Settings }
            ].map((tab) => {
              const IconComp = tab.icon;
              return (
                <button 
                  key={tab.id}
                  onClick={() => setDocTab(tab.id as any)}
                  className={`flex items-center gap-3 px-4 py-3 rounded text-left font-mono text-xs transition-colors ${docTab === tab.id ? "bg-zinc-900 text-white font-bold" : "text-zinc-400 hover:text-white hover:bg-zinc-950/60"}`}
                >
                  <IconComp className="w-4 h-4 flex-shrink-0" />
                  {tab.label}
                </button>
              );
            })}
          </div>

          {/* Doc view canvas */}
          <div className="lg:col-span-9">
            <div className="glass-card rounded-lg border border-zinc-900 p-8 bg-zinc-950/20 font-sans leading-relaxed text-zinc-300 text-sm max-h-[500px] overflow-y-auto">
              {docTab === "quickstart" && (
                <div className="flex flex-col gap-6">
                  <h3 className="text-xl font-bold text-white border-b border-zinc-900 pb-3 font-mono">DEVELOPER_INTEGRATION_GUIDE</h3>
                  <p>This details the ingestion API session configuration. Use this pattern to integrate client uploader libraries to regional gateways.</p>
                  
                  <div>
                    <h4 className="text-xs font-mono font-bold text-white mb-2">// 1. REQUEST PRESIGNED UPLOAD SESSION</h4>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`POST /api/jobs/upload-session
Payload:
{
  "file_size_bytes": 104857600, // 100MB
  "file_name": "source.mp4"
}

Response (Session Token & Partition ID info):
{
  "job_id": "us-east:a6f9f38f-...",
  "session_token": "eyJhbGciOi...",
  "part_size": 26214400,
  "total_parts": 4
}`}
                    </pre>
                  </div>

                  <div>
                    <h4 className="text-xs font-mono font-bold text-white mb-2">// 2. REQUEST PRESIGNED UPLOAD URL BATCH</h4>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`POST /api/jobs/{job_id}/urls?start=1&count=4
Headers:
Authorization: Bearer <session_token>

Response:
{
  "part_numbers": [1, 2, 3, 4],
  "urls": [
    "http://minio:9000/transcoder/jobs/part_1...",
    "http://minio:9000/transcoder/jobs/part_2..."
  ]
}`}
                    </pre>
                  </div>
                </div>
              )}

              {docTab === "deployment" && (
                <div className="flex flex-col gap-6">
                  <h3 className="text-xl font-bold text-white border-b border-zinc-900 pb-3 font-mono">MULTI_REGION_DEPLOYMENT_GUIDE</h3>
                  <p>Network layout structures and bucket-to-bucket cross-region replication configs to enforce data gravity constraints.</p>
                  
                  <div>
                    <h4 className="text-xs font-mono font-bold text-white mb-2">// AWS S3 CRR METADATA POLICY</h4>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`{
  "Role": "arn:aws:iam::123456789012:role/S3TranscoderReplicationRole",
  "Rules": [
    {
      "ID": "ReplicateCompletedManifestsOnly",
      "Status": "Enabled",
      "Filter": {
        "And": {
          "Prefix": "jobs/",
          "Tags": [
            { "Key": "replicate", "Value": "true" }
          ]
        }
      },
      "Destination": {
        "Bucket": "arn:aws:s3:::transcoder-eu-west"
      }
    }
  ]
}`}
                    </pre>
                  </div>

                  <p className="text-xs text-zinc-500">
                    Note: Video chunks (.ts) remain local to the source region's bucket. Only the master .m3u8 playlist file is tagged with replicate=true to initiate the AWS replication rules.
                  </p>
                </div>
              )}

              {docTab === "isolation" && (
                <div className="flex flex-col gap-6">
                  <h3 className="text-xl font-bold text-white border-b border-zinc-900 pb-3 font-mono">REGIONAL_CONFIG_SCHEMA</h3>
                  <p>Configuration profile settings to define backing database locations, etcd lock TTLs, and NATS JetStream server endpoints.</p>
                  
                  <div>
                    <h4 className="text-xs font-mono font-bold text-white mb-2">// configs/us-east.yaml</h4>
                    <pre className="border border-zinc-900 bg-zinc-950 p-4 rounded text-xs font-mono text-zinc-400 overflow-x-auto">
{`role: "gateway"
region: "us-east"

redis:
  addrs: ["127.0.0.1:6379"]
  pool_size: 10

nats:
  urls: ["nats://127.0.0.1:4222"]

etcd:
  endpoints: ["127.0.0.1:2379"]

object_store:
  endpoint: "127.0.0.1:9000"
  bucket: "transcoder-us-east"

coordinator:
  partition_count: 4
  slicing_lock_ttl_sec: 5
  etcd_lease_ttl_sec: 5

worker:
  scratch_dir: "/tmp/scratch-us-east"
  min_disk_free_gb: 1
  watchdog_interval_sec: 2
  max_task_duration_min: 2
  concurrent_tasks: 4`}
                    </pre>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      </section>

      {/* Footer */}
      <footer className="border-t border-zinc-900/60 py-12 px-6 bg-zinc-950/20 text-center font-mono text-[10px] text-zinc-600">
        <div className="max-w-7xl mx-auto flex flex-col md:flex-row justify-between items-center gap-4">
          <span>&copy; 2026 DISTRIBUTED VOD PLATFORM. ALL RIGHTS RESERVED.</span>
          <div className="flex gap-4">
            <span className="text-zinc-500">VERSION 1.2.0-STARK-MINIMAL</span>
            <span className="text-zinc-500">|</span>
            <span className="text-zinc-500">DEPLOYMENT: MULTI_REGION_ACTIVE</span>
          </div>
        </div>
      </footer>
    </main>
  );
}
