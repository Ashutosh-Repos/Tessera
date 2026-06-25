import { useState, useRef, useEffect, useCallback } from 'react';
import Hls from 'hls.js';
import type { Level } from 'hls.js';
import { 
  Play, 
  Pause, 
  Volume2, 
  VolumeX, 
  Activity, 
  Maximize, 
  Minimize, 
  SlidersHorizontal 
} from 'lucide-react';

interface VideoPlayerProps {
  hlsUrl: string;
  poster?: string;
  autoplay?: boolean;
}

interface StreamDiagnostics {
  bitrate: number;
  bufferLength: number;
  droppedFrames: number;
  resolution: string;
  latency: number;
  level: number;
  totalLevels: number;
}

export const VideoPlayer: React.FC<VideoPlayerProps> = ({ hlsUrl, poster, autoplay = false }) => {
  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const progressRef = useRef<HTMLDivElement>(null);
  const hlsRef = useRef<Hls | null>(null);

  const [isPlaying, setIsPlaying] = useState(false);
  const [isMuted, setIsMuted] = useState(false);
  const [isBuffering, setIsBuffering] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [showControls, setShowControls] = useState(true);
  const [showDiagnostics, setShowDiagnostics] = useState(false);
  const [showQualityMenu, setShowQualityMenu] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [buffered, setBuffered] = useState(0);
  const [volume, setVolume] = useState(1);
  const [levels, setLevels] = useState<Level[]>([]);
  const [currentLevel, setCurrentLevel] = useState(-1); // -1 = auto
  const [diagnostics, setDiagnostics] = useState<StreamDiagnostics>({
    bitrate: 0, bufferLength: 0, droppedFrames: 0,
    resolution: '—', latency: 0, level: 0, totalLevels: 0,
  });

  const controlsTimeoutRef = useRef<number>(0);

  // ── HLS.js Initialization ──
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !hlsUrl) return;

    if (Hls.isSupported()) {
      const hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
        startLevel: -1, // auto
      });

      hls.loadSource(hlsUrl);
      hls.attachMedia(video);

      hls.on(Hls.Events.MANIFEST_PARSED, (_event, data) => {
        setLevels(data.levels);
        if (autoplay) video.play().catch(() => {});
      });

      hls.on(Hls.Events.LEVEL_SWITCHED, (_event, data) => {
        setCurrentLevel(data.level);
      });

      hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
        const loadTime = data.frag.stats.loading.end - data.frag.stats.loading.start;
        setDiagnostics(prev => ({ ...prev, latency: Math.round(loadTime) }));
      });

      let mediaErrorCount = 0;
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              console.error('Fatal network error, trying to recover...', data);
              hls.startLoad();
              break;
            case Hls.ErrorTypes.MEDIA_ERROR:
              mediaErrorCount++;
              if (mediaErrorCount <= 3) {
                console.warn(`Fatal media error (attempt ${mediaErrorCount}), trying to recover...`, data);
                hls.recoverMediaError();
              } else {
                console.error('Fatal media error, recovery attempts exhausted. Destroying player...', data);
                hls.destroy();
              }
              break;
            default:
              console.error('Fatal unrecoverable error, destroying player...', data);
              hls.destroy();
              break;
          }
        }
      });

      hlsRef.current = hls;

      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      // Safari native HLS
      video.src = hlsUrl;
      if (autoplay) video.play().catch(() => {});
    }
  }, [hlsUrl, autoplay]);

  // ── Diagnostics Polling ──
  useEffect(() => {
    if (!showDiagnostics) return;

    const interval = setInterval(() => {
      const video = videoRef.current;
      const hls = hlsRef.current;
      if (!video || !hls) return;

      const quality = video.getVideoPlaybackQuality?.();
      const currentLvl = hls.levels[hls.currentLevel];
      const bufLen = video.buffered.length > 0
        ? video.buffered.end(video.buffered.length - 1) - video.currentTime
        : 0;

      setDiagnostics({
        bitrate: currentLvl ? Math.round(currentLvl.bitrate / 1000) : 0,
        bufferLength: Math.round(bufLen * 10) / 10,
        droppedFrames: quality?.droppedVideoFrames ?? 0,
        resolution: currentLvl ? `${currentLvl.width}×${currentLvl.height}` : '—',
        latency: diagnostics.latency,
        level: hls.currentLevel,
        totalLevels: hls.levels.length,
      });
    }, 500);

    return () => clearInterval(interval);
  }, [showDiagnostics, diagnostics.latency]);

  // ── Video Event Handlers ──
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const onPlay = () => {
      setIsPlaying(true);
      setIsBuffering(false);
    };
    const onPause = () => {
      setIsPlaying(false);
      setIsBuffering(false);
    };
    const onWaiting = () => setIsBuffering(true);
    const onPlaying = () => setIsBuffering(false);
    const onTimeUpdate = () => {
      setCurrentTime(video.currentTime);
      if (video.buffered.length > 0) {
        setBuffered(video.buffered.end(video.buffered.length - 1));
      }
    };
    const onLoadedMetadata = () => setDuration(video.duration);
    const onVolumeChange = () => {
      setVolume(video.volume);
      setIsMuted(video.muted);
    };

    video.addEventListener('play', onPlay);
    video.addEventListener('pause', onPause);
    video.addEventListener('waiting', onWaiting);
    video.addEventListener('playing', onPlaying);
    video.addEventListener('timeupdate', onTimeUpdate);
    video.addEventListener('loadedmetadata', onLoadedMetadata);
    video.addEventListener('volumechange', onVolumeChange);

    return () => {
      video.removeEventListener('play', onPlay);
      video.removeEventListener('pause', onPause);
      video.removeEventListener('waiting', onWaiting);
      video.removeEventListener('playing', onPlaying);
      video.removeEventListener('timeupdate', onTimeUpdate);
      video.removeEventListener('loadedmetadata', onLoadedMetadata);
      video.removeEventListener('volumechange', onVolumeChange);
    };
  }, []);

  // ── Keyboard Shortcuts ──
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const video = videoRef.current;
      if (!video) return;
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;

      switch (e.key.toLowerCase()) {
        case ' ':
        case 'k':
          e.preventDefault();
          video.paused ? video.play() : video.pause();
          break;
        case 'f':
          e.preventDefault();
          toggleFullscreen();
          break;
        case 'm':
          e.preventDefault();
          video.muted = !video.muted;
          break;
        case 'arrowleft':
          e.preventDefault();
          video.currentTime = Math.max(0, video.currentTime - 10);
          break;
        case 'arrowright':
          e.preventDefault();
          video.currentTime = Math.min(video.duration, video.currentTime + 10);
          break;
        case 'arrowup':
          e.preventDefault();
          video.volume = Math.min(1, video.volume + 0.1);
          break;
        case 'arrowdown':
          e.preventDefault();
          video.volume = Math.max(0, video.volume - 0.1);
          break;
        case 'd':
          e.preventDefault();
          setShowDiagnostics(prev => !prev);
          break;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  // ── Fullscreen Change Detection ──
  useEffect(() => {
    const handler = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', handler);
    return () => document.removeEventListener('fullscreenchange', handler);
  }, []);

  // ── Controls Auto-Hide ──
  const showControlsTemporarily = useCallback(() => {
    setShowControls(true);
    clearTimeout(controlsTimeoutRef.current);
    controlsTimeoutRef.current = window.setTimeout(() => {
      if (videoRef.current && !videoRef.current.paused) {
        setShowControls(false);
      }
    }, 3000);
  }, []);

  // ── Actions ──
  const togglePlay = () => {
    const video = videoRef.current;
    if (!video) return;
    video.paused ? video.play() : video.pause();
  };

  const toggleMute = () => {
    const video = videoRef.current;
    if (!video) return;
    video.muted = !video.muted;
  };

  const toggleFullscreen = () => {
    const container = containerRef.current;
    if (!container) return;
    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else {
      container.requestFullscreen();
    }
  };

  const seekTo = (e: React.MouseEvent<HTMLDivElement>) => {
    const video = videoRef.current;
    const bar = progressRef.current;
    if (!video || !bar) return;
    const rect = bar.getBoundingClientRect();
    const pct = (e.clientX - rect.left) / rect.width;
    video.currentTime = pct * video.duration;
  };

  const setQualityLevel = (level: number) => {
    const hls = hlsRef.current;
    if (!hls) return;
    hls.currentLevel = level; // -1 = auto
    setCurrentLevel(level);
    setShowQualityMenu(false);
  };

  const formatTime = (seconds: number): string => {
    if (!isFinite(seconds) || isNaN(seconds)) return '0:00';
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    return `${m}:${s.toString().padStart(2, '0')}`;
  };

  const getQualityLabel = (level: number): string => {
    if (level === -1) return 'Auto';
    const lvl = levels[level];
    if (!lvl) return `Level ${level}`;
    return `${lvl.height}p`;
  };

  const progressPct = duration > 0 ? (currentTime / duration) * 100 : 0;
  const bufferedPct = duration > 0 ? (buffered / duration) * 100 : 0;

  return (
    <div
      className={`relative w-full aspect-video rounded-xl overflow-hidden border border-zinc-800 bg-black group select-none shadow-2xl transition-all duration-300 ${
        isFullscreen ? 'rounded-none border-0' : ''
      }`}
      ref={containerRef}
      onMouseMove={showControlsTemporarily}
      onMouseLeave={() => { if (isPlaying) setShowControls(false); }}
    >
      <video
        ref={videoRef}
        className="w-full h-full object-cover cursor-pointer"
        poster={poster}
        onClick={togglePlay}
        playsInline
      />

      {/* Play Overlay (shown when paused) */}
      {!isPlaying && (
        <div 
          className="absolute inset-0 flex items-center justify-center bg-black/40 cursor-pointer transition-opacity duration-300 hover:bg-black/30"
          onClick={togglePlay}
        >
          <div className="rounded-full bg-zinc-950/80 p-5 text-white ring-1 ring-zinc-800/80 backdrop-blur-md transition-transform duration-200 hover:scale-110 active:scale-95 shadow-2xl shadow-black/50">
            <Play className="h-8 w-8 fill-current translate-x-0.5" />
          </div>
        </div>
      )}

      {/* Buffering Spinner Overlay */}
      {isBuffering && isPlaying && (
        <div className="absolute inset-0 flex items-center justify-center bg-black/50 backdrop-blur-sm pointer-events-none z-10">
          <div className="relative flex h-14 w-14 items-center justify-center">
            <div className="h-full w-full rounded-full border-4 border-purple-500/20 border-t-purple-500 animate-spin" />
          </div>
        </div>
      )}

      {/* Diagnostics Overlay */}
      {showDiagnostics && (
        <div className="absolute top-4 left-4 bg-zinc-950/85 backdrop-blur-md border border-zinc-800 rounded-lg p-4 w-64 text-xs font-mono text-zinc-300 z-20 space-y-1.5 pointer-events-none shadow-xl shadow-black/80">
          <div className="text-zinc-400 font-bold border-b border-zinc-800 pb-1.5 mb-2 flex items-center gap-1.5">
            <Activity className="h-3.5 w-3.5 text-purple-400 animate-pulse" />
            Stream Diagnostics
          </div>
          <div className="flex justify-between border-b border-zinc-900/50 pb-1">
            <span>Resolution</span><span className="font-semibold text-zinc-100">{diagnostics.resolution}</span>
          </div>
          <div className="flex justify-between border-b border-zinc-900/50 pb-1">
            <span>Bitrate</span><span className="font-semibold text-zinc-100">{diagnostics.bitrate} Kbps</span>
          </div>
          <div className="flex justify-between border-b border-zinc-900/50 pb-1">
            <span>Buffer Length</span><span className="font-semibold text-zinc-100">{diagnostics.bufferLength}s</span>
          </div>
          <div className="flex justify-between border-b border-zinc-900/50 pb-1">
            <span>Dropped Frames</span><span className="font-semibold text-zinc-100">{diagnostics.droppedFrames}</span>
          </div>
          <div className="flex justify-between border-b border-zinc-900/50 pb-1">
            <span>Segment Latency</span><span className="font-semibold text-zinc-100">{diagnostics.latency}ms</span>
          </div>
          <div className="flex justify-between">
            <span>ABR Level</span><span className="font-semibold text-zinc-100">{diagnostics.level + 1}/{diagnostics.totalLevels}</span>
          </div>
        </div>
      )}

      {/* Control Bar */}
      <div 
        className={`absolute bottom-0 inset-x-0 bg-gradient-to-t from-black/95 via-black/70 to-transparent px-4 pb-4 pt-16 flex flex-col gap-3 transition-all duration-300 z-10 ${
          showControls ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-2 pointer-events-none'
        }`}
      >
        {/* Progress Bar */}
        <div 
          className="relative h-1 bg-zinc-800/80 rounded-full cursor-pointer hover:h-2 transition-all duration-150 group/progress" 
          ref={progressRef} 
          onClick={seekTo}
        >
          <div className="absolute inset-y-0 left-0 bg-zinc-700/50 rounded-full" style={{ width: `${bufferedPct}%` }} />
          <div 
            className="absolute inset-y-0 left-0 bg-gradient-to-r from-purple-500 to-pink-500 rounded-full flex items-center justify-end" 
            style={{ width: `${progressPct}%` }}
          >
            <div className="absolute -right-1.5 h-3.5 w-3.5 scale-0 group-hover/progress:scale-100 rounded-full bg-white ring-2 ring-purple-600 transition-transform duration-100 shadow-md" />
          </div>
        </div>

        <div className="flex justify-between items-center">
          {/* Left Controls */}
          <div className="flex items-center gap-3">
            <button 
              className="p-1.5 text-zinc-300 hover:text-white hover:bg-zinc-800/50 rounded-md transition-colors" 
              onClick={togglePlay} 
              aria-label={isPlaying ? 'Pause' : 'Play'}
            >
              {isPlaying ? (
                <Pause className="h-5 w-5 fill-current" />
              ) : (
                <Play className="h-5 w-5 fill-current" />
              )}
            </button>

            {/* Volume Control */}
            <div className="flex items-center gap-1 group/volume">
              <button 
                className="p-1.5 text-zinc-300 hover:text-white hover:bg-zinc-800/50 rounded-md transition-colors" 
                onClick={toggleMute} 
                aria-label={isMuted ? 'Unmute' : 'Mute'}
              >
                {isMuted || volume === 0 ? (
                  <VolumeX className="h-5 w-5" />
                ) : (
                  <Volume2 className="h-5 w-5" />
                )}
              </button>
              <input
                type="range"
                className="w-0 opacity-0 group-hover/volume:w-16 group-hover/volume:opacity-100 transition-all duration-200 h-1 accent-purple-500 bg-zinc-700 rounded-full appearance-none cursor-pointer"
                min="0"
                max="1"
                step="0.05"
                value={isMuted ? 0 : volume}
                onChange={(e) => {
                  const video = videoRef.current;
                  if (!video) return;
                  const newVol = parseFloat(e.target.value);
                  video.volume = newVol;
                  if (newVol > 0) {
                    video.muted = false;
                  } else {
                    video.muted = true;
                  }
                }}
              />
            </div>

            <span className="text-xs font-mono text-zinc-400 select-none">
              {formatTime(currentTime)} / {formatTime(duration)}
            </span>
          </div>

          {/* Right Controls */}
          <div className="flex items-center gap-2">
            {/* Toggle Diagnostics */}
            <button
              className={`p-1.5 rounded-md transition-colors ${
                showDiagnostics 
                  ? 'text-purple-400 bg-purple-950/30 border border-purple-900/50' 
                  : 'text-zinc-300 hover:text-white hover:bg-zinc-800/50 border border-transparent'
              }`}
              onClick={() => setShowDiagnostics(prev => !prev)}
              aria-label="Toggle diagnostics"
            >
              <Activity className="h-5 w-5" />
            </button>

            {/* Quality Selector */}
            <div className="relative">
              <button
                className={`flex items-center gap-1 px-2.5 py-1 text-xs font-mono rounded-md border transition-colors ${
                  showQualityMenu 
                    ? 'text-purple-400 bg-purple-950/30 border-purple-900/50'
                    : 'text-zinc-300 hover:text-white hover:bg-zinc-800/50 border-zinc-800/60'
                }`}
                onClick={() => setShowQualityMenu(prev => !prev)}
                aria-label="Quality"
              >
                <SlidersHorizontal className="h-3.5 w-3.5 mr-0.5" />
                {getQualityLabel(currentLevel)}
              </button>
              {showQualityMenu && (
                <div className="absolute bottom-10 right-0 bg-zinc-950/95 border border-zinc-800 rounded-lg p-1 shadow-2xl w-32 flex flex-col gap-0.5 backdrop-blur-md z-30 animate-in fade-in slide-in-from-bottom-2 duration-150">
                  <button
                    className={`w-full text-left px-2.5 py-1.5 text-xs font-mono rounded hover:bg-zinc-900 text-zinc-300 hover:text-white flex justify-between items-center transition-colors ${
                      currentLevel === -1 ? 'bg-purple-950/30 text-purple-400 border-l border-purple-500 pl-2' : ''
                    }`}
                    onClick={() => setQualityLevel(-1)}
                  >
                    Auto
                  </button>
                  {levels.map((lvl, i) => (
                    <button
                      key={i}
                      className={`w-full text-left px-2.5 py-1.5 text-xs font-mono rounded hover:bg-zinc-900 text-zinc-300 hover:text-white flex flex-col gap-0.5 transition-colors ${
                        currentLevel === i ? 'bg-purple-950/30 text-purple-400 border-l border-purple-500 pl-2' : ''
                      }`}
                      onClick={() => setQualityLevel(i)}
                    >
                      <span>{lvl.height}p</span>
                      <span className="text-[9px] text-zinc-500">{Math.round(lvl.bitrate / 1000)} Kbps</span>
                    </button>
                  ))}
                </div>
              )}
            </div>

            {/* Toggle Fullscreen */}
            <button 
              className="p-1.5 text-zinc-300 hover:text-white hover:bg-zinc-800/50 rounded-md transition-colors" 
              onClick={toggleFullscreen} 
              aria-label={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
            >
              {isFullscreen ? (
                <Minimize className="h-5 w-5" />
              ) : (
                <Maximize className="h-5 w-5" />
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
