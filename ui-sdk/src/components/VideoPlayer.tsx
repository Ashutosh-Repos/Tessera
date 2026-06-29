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
import { cn } from '../lib/utils';

export interface VideoPlayerClassNames {
  container?: string;
  video?: string;
  playOverlay?: string;
  playButton?: string;
  bufferingOverlay?: string;
  diagnosticsPanel?: string;
  controlBar?: string;
  progressBarContainer?: string;
  progressBar?: string;
  volumeSlider?: string;
  qualitySelectorButton?: string;
  qualitySelectorMenu?: string;
  fullscreenButton?: string;
}

interface VideoPlayerProps {
  hlsUrl: string;
  poster?: string;
  autoplay?: boolean;
  className?: string;
  classNames?: VideoPlayerClassNames;
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

export const VideoPlayer: React.FC<VideoPlayerProps> = ({ 
  hlsUrl, 
  poster, 
  autoplay = false,
  className,
  classNames = {}
}) => {
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
      className={cn(
        "relative w-full aspect-video rounded-md overflow-hidden border border-neutral-800 bg-black group select-none shadow-2xl transition-all duration-300",
        isFullscreen && "rounded-none border-0",
        className,
        classNames.container
      )}
      ref={containerRef}
      onMouseMove={showControlsTemporarily}
      onMouseLeave={() => { if (isPlaying) setShowControls(false); }}
    >
      <video
        ref={videoRef}
        className={cn("w-full h-full object-cover cursor-pointer", classNames.video)}
        poster={poster}
        onClick={togglePlay}
        playsInline
      />

      {/* Play Overlay (shown when paused) */}
      {!isPlaying && (
        <div 
          className={cn(
            "absolute inset-0 flex items-center justify-center bg-black/55 cursor-pointer transition-opacity duration-300",
            classNames.playOverlay
          )}
          onClick={togglePlay}
        >
          <button className={cn("rounded-full bg-white p-5 text-black transition-transform duration-200 hover:scale-105 active:scale-95 shadow-xl", classNames.playButton)}>
            <Play className="h-6 w-6 fill-current translate-x-0.5" />
          </button>
        </div>
      )}

      {/* Buffering Spinner Overlay */}
      {isBuffering && isPlaying && (
        <div className={cn("absolute inset-0 flex items-center justify-center bg-black/60 pointer-events-none z-10", classNames.bufferingOverlay)}>
          <div className="h-8 w-8 rounded-full border-2 border-neutral-850 border-t-white animate-spin" />
        </div>
      )}

      {/* Diagnostics Overlay */}
      {showDiagnostics && (
        <div className={cn(
          "absolute top-4 left-4 bg-neutral-950/90 border border-neutral-850 rounded p-4 w-60 text-[10px] font-mono text-neutral-300 z-20 space-y-1.5 pointer-events-none shadow-xl",
          classNames.diagnosticsPanel
        )}>
          <div className="text-white font-bold border-b border-neutral-850 pb-1.5 mb-2 flex items-center gap-1.5">
            <Activity className="h-3 w-3 text-white" />
            STREAM DIAGNOSTICS
          </div>
          <div className="flex justify-between border-b border-neutral-900 pb-1">
            <span>Resolution</span><span className="font-semibold text-white">{diagnostics.resolution}</span>
          </div>
          <div className="flex justify-between border-b border-neutral-900 pb-1">
            <span>Bitrate</span><span className="font-semibold text-white">{diagnostics.bitrate} Kbps</span>
          </div>
          <div className="flex justify-between border-b border-neutral-900 pb-1">
            <span>Buffer Length</span><span className="font-semibold text-white">{diagnostics.bufferLength}s</span>
          </div>
          <div className="flex justify-between border-b border-neutral-900 pb-1">
            <span>Dropped Frames</span><span className="font-semibold text-white">{diagnostics.droppedFrames}</span>
          </div>
          <div className="flex justify-between border-b border-neutral-900 pb-1">
            <span>Segment Latency</span><span className="font-semibold text-white">{diagnostics.latency}ms</span>
          </div>
          <div className="flex justify-between">
            <span>ABR Level</span><span className="font-semibold text-white">{diagnostics.level + 1}/{diagnostics.totalLevels}</span>
          </div>
        </div>
      )}

      {/* Control Bar */}
      <div 
        className={cn(
          "absolute bottom-0 inset-x-0 bg-gradient-to-t from-black via-black/85 to-transparent px-4 pb-4 pt-16 flex flex-col gap-3 transition-all duration-300 z-10",
          showControls ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2 pointer-events-none",
          classNames.controlBar
        )}
      >
        {/* Progress Bar */}
        <div 
          className={cn(
            "relative h-1 bg-neutral-800 rounded-full cursor-pointer hover:h-1.5 transition-all duration-150 group/progress",
            classNames.progressBarContainer
          )}
          ref={progressRef} 
          onClick={seekTo}
        >
          <div className="absolute inset-y-0 left-0 bg-neutral-700/60 rounded-full" style={{ width: `${bufferedPct}%` }} />
          <div 
            className={cn("absolute inset-y-0 left-0 bg-white rounded-full flex items-center justify-end", classNames.progressBar)}
            style={{ width: `${progressPct}%` }}
          />
        </div>

        <div className="flex justify-between items-center">
          {/* Left Controls */}
          <div className="flex items-center gap-3">
            <button 
              className="p-1 text-neutral-400 hover:text-white transition-colors" 
              onClick={togglePlay} 
              aria-label={isPlaying ? 'Pause' : 'Play'}
            >
              {isPlaying ? <Pause className="h-4 w-4 fill-current" /> : <Play className="h-4 w-4 fill-current" />}
            </button>

            {/* Volume Control */}
            <div className="flex items-center gap-1.5 group/volume">
              <button 
                className="p-1 text-neutral-400 hover:text-white transition-colors" 
                onClick={toggleMute} 
                aria-label={isMuted ? 'Unmute' : 'Mute'}
              >
                {isMuted || volume === 0 ? <VolumeX className="h-4 w-4" /> : <Volume2 className="h-4 w-4" />}
              </button>
              <input
                type="range"
                className={cn(
                  "w-0 opacity-0 group-hover/volume:w-16 group-hover/volume:opacity-100 transition-all duration-200 h-0.5 accent-white bg-neutral-850 rounded-full appearance-none cursor-pointer",
                  classNames.volumeSlider
                )}
                min="0"
                max="1"
                step="0.05"
                value={isMuted ? 0 : volume}
                onChange={(e) => {
                  const video = videoRef.current;
                  if (!video) return;
                  const newVol = parseFloat(e.target.value);
                  video.volume = newVol;
                  video.muted = newVol === 0;
                }}
              />
            </div>

            <span className="text-[10px] font-mono text-neutral-400 select-none">
              {formatTime(currentTime)} / {formatTime(duration)}
            </span>
          </div>

          {/* Right Controls */}
          <div className="flex items-center gap-3">
            {/* Toggle Diagnostics */}
            <button
              className={cn(
                "p-1 rounded transition-colors",
                showDiagnostics ? "text-white" : "text-neutral-400 hover:text-white"
              )}
              onClick={() => setShowDiagnostics(prev => !prev)}
              aria-label="Toggle diagnostics"
            >
              <Activity className="h-4 w-4" />
            </button>

            {/* Quality Selector */}
            <div className="relative">
              <button
                className={cn(
                  "flex items-center gap-1 px-2 py-1 text-[10px] font-mono rounded border border-neutral-800 text-neutral-400 hover:text-white hover:bg-neutral-900 transition-colors",
                  showQualityMenu && "text-white border-neutral-750 bg-neutral-900",
                  classNames.qualitySelectorButton
                )}
                onClick={() => setShowQualityMenu(prev => !prev)}
                aria-label="Quality"
              >
                <SlidersHorizontal className="h-3 w-3" />
                {getQualityLabel(currentLevel)}
              </button>
              {showQualityMenu && (
                <div className={cn(
                  "absolute bottom-8 right-0 bg-neutral-950 border border-neutral-800 rounded p-1 shadow-2xl w-28 flex flex-col gap-0.5 z-30 animate-in fade-in slide-in-from-bottom-1 duration-100",
                  classNames.qualitySelectorMenu
                )}>
                  <button
                    className={cn(
                      "w-full text-left px-2 py-1.5 text-[10px] font-mono rounded hover:bg-neutral-900 text-neutral-400 hover:text-white flex justify-between items-center transition-colors",
                      currentLevel === -1 && "text-white font-semibold"
                    )}
                    onClick={() => setQualityLevel(-1)}
                  >
                    Auto
                  </button>
                  {levels.map((lvl, i) => (
                    <button
                      key={i}
                      className={cn(
                        "w-full text-left px-2 py-1.5 text-[10px] font-mono rounded hover:bg-neutral-900 text-neutral-400 hover:text-white flex flex-col gap-0.5 transition-colors",
                        currentLevel === i && "text-white font-semibold"
                      )}
                      onClick={() => setQualityLevel(i)}
                    >
                      <span>{lvl.height}p</span>
                      <span className="text-[8px] text-neutral-500">{Math.round(lvl.bitrate / 1000)} Kbps</span>
                    </button>
                  ))}
                </div>
              )}
            </div>

            {/* Toggle Fullscreen */}
            <button 
              className={cn("p-1 text-neutral-400 hover:text-white transition-colors", classNames.fullscreenButton)} 
              onClick={toggleFullscreen} 
              aria-label={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
            >
              {isFullscreen ? <Minimize className="h-4 w-4" /> : <Maximize className="h-4 w-4" />}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
