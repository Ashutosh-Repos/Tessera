import { useState, useRef, useEffect, useCallback, useMemo } from 'react';
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
  SlidersHorizontal,
  Gauge,
  PictureInPicture2,
  RotateCcw,
  RotateCw,
  HelpCircle,
  X
} from 'lucide-react';
import { cn } from './utils';
import { parseVTTCues, type SpriteCue } from './vtt';

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
  previewTooltip?: string;
  volumeSlider?: string;
  qualitySelectorButton?: string;
  qualitySelectorMenu?: string;
  speedSelectorButton?: string;
  speedSelectorMenu?: string;
  fullscreenButton?: string;
  pipButton?: string;
  seekRipple?: string;
  timeDisplay?: string;
  helpModal?: string;
  controlButtonLeft?: string;
  controlButtonRight?: string;
}

export interface SpriteConfig {
  width: number;
  height: number;
  cols: number;
  intervalSec?: number;
}

export interface VideoPlayerProps {
  hlsUrl?: string;
  gatewayUrl?: string;      // Backend Gateway URL, e.g. http://localhost:8080
  jobId?: string;           // Transcoding Job ID, e.g. job_us-east:1234
  poster?: string;
  autoplay?: boolean;
  spriteUrl?: string;       // Direct sprite image URL or WebVTT URL
  spriteVttUrl?: string;    // WebVTT metadata file for thumbnails
  spriteConfig?: SpriteConfig; // Config if using sprite grid directly without VTT
  className?: string;
  classNames?: VideoPlayerClassNames;

  // Customization Props
  accentColor?: string;
  controlsAutoHideMs?: number;
  borderRadius?: number;
  aspectRatio?: string;
  controlsPosition?: "overlay" | "below";
  controlsBg?: string;
  controlsBgOpacity?: number;
  controlsIconColor?: string;
  progressBarHeight?: number;
  showControlsOnHover?: boolean;

  // Visibility Controls
  showPlayOverlay?: boolean;
  showDiagnosticsButton?: boolean;
  showSpeedSelector?: boolean;
  showQualitySelector?: boolean;
  showPipButton?: boolean;
  showHelpButton?: boolean;
  showVolumeSlider?: boolean;
  showTimeDisplay?: boolean;
  showSeekRipple?: boolean;

  // Theme
  theme?: 'dark' | 'light';
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
  spriteUrl,
  spriteVttUrl,
  spriteConfig = { width: 160, height: 90, cols: 10, intervalSec: 5 },
  className,
  classNames = {},

  accentColor = '#ffffff',
  controlsAutoHideMs = 3000,
  borderRadius = 8,
  aspectRatio = '16/9',
  controlsPosition = 'overlay',
  controlsBg = '#000000',
  controlsBgOpacity = 70,
  controlsIconColor = '#ffffff',
  progressBarHeight = 6,
  showControlsOnHover = true,

  showPlayOverlay = true,
  showDiagnosticsButton = true,
  showSpeedSelector = true,
  showQualitySelector = true,
  showPipButton = true,
  showHelpButton = true,
  showVolumeSlider = true,
  showTimeDisplay = true,
  showSeekRipple = true,
  theme = 'dark',
}) => {
  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const progressRef = useRef<HTMLDivElement>(null);
  const hlsRef = useRef<Hls | null>(null);

  // Core State
  const [isPlaying, setIsPlaying] = useState(false);
  const [isMuted, setIsMuted] = useState(false);
  const [isBuffering, setIsBuffering] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [isPip, setIsPip] = useState(false);
  const [isPipSupported, setIsPipSupported] = useState(false);

  useEffect(() => {
    if (typeof window !== 'undefined' && document.pictureInPictureEnabled) {
      setIsPipSupported(true);
    }
  }, []);
  const [showControls, setShowControls] = useState(true);
  const [showDiagnostics, setShowDiagnostics] = useState(false);
  const [showHelpModal, setShowHelpModal] = useState(false);
  const [showQualityMenu, setShowQualityMenu] = useState(false);
  const [showSpeedMenu, setShowSpeedMenu] = useState(false);
  const [playbackRate, setPlaybackRate] = useState<number>(1);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [buffered, setBuffered] = useState(0);
  const [volume, setVolume] = useState(1);
  const [levels, setLevels] = useState<Level[]>([]);
  const [currentLevel, setCurrentLevel] = useState(-1); // -1 = auto
  
  // Ripple Feedback Animation State
  const [seekRipple, setSeekRipple] = useState<{ type: 'rewind' | 'forward'; id: number } | null>(null);

  // Sprite / VTT Thumbnail Scrubbing State
  const [vttCues, setVttCues] = useState<SpriteCue[]>([]);
  const [hoverState, setHoverState] = useState<{
    visible: boolean;
    time: number;
    percent: number;
    xPx: number;
  }>({ visible: false, time: 0, percent: 0, xPx: 0 });

  // Stream Diagnostics
  const [diagnostics, setDiagnostics] = useState<StreamDiagnostics>({
    bitrate: 0, bufferLength: 0, droppedFrames: 0,
    resolution: '—', latency: 0, level: 0, totalLevels: 0,
  });

  const controlsTimeoutRef = useRef<number>(0);

  // ── 1. Fetch & Parse WebVTT Sprite Data ──
  useEffect(() => {
    const targetVtt = spriteVttUrl || (spriteUrl && spriteUrl.endsWith('.vtt') ? spriteUrl : undefined);
    if (!targetVtt) return;

    let isSubscribed = true;
    fetch(targetVtt)
      .then(res => res.ok ? res.text() : '')
      .then(text => {
        if (!isSubscribed || !text) return;
        const parsed = parseVTTCues(text, targetVtt);
        setVttCues(parsed);
      })
      .catch(err => console.warn('Failed to load sprite VTT:', err));

    return () => { isSubscribed = false; };
  }, [spriteUrl, spriteVttUrl]);

  // ── 2. HLS.js Initialization & Adaptive Switching ──
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
                console.warn(`Fatal media error (attempt ${mediaErrorCount}), recovering...`, data);
                hls.recoverMediaError();
              } else {
                console.error('Media error recovery exhausted. Destroying player...', data);
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
      video.src = hlsUrl;
      if (autoplay) video.play().catch(() => {});
    }
  }, [hlsUrl, autoplay]);

  // ── 3. Diagnostics Polling ──
  useEffect(() => {
    if (!showDiagnostics) return;

    const interval = setInterval(() => {
      const video = videoRef.current;
      const hls = hlsRef.current;
      if (!video) return;

      const quality = video.getVideoPlaybackQuality?.();
      const currentLvl = hls ? hls.levels[hls.currentLevel] : null;
      const bufLen = video.buffered.length > 0
        ? video.buffered.end(video.buffered.length - 1) - video.currentTime
        : 0;

      setDiagnostics({
        bitrate: currentLvl ? Math.round(currentLvl.bitrate / 1000) : 0,
        bufferLength: Math.round(bufLen * 10) / 10,
        droppedFrames: quality?.droppedVideoFrames ?? 0,
        resolution: currentLvl ? `${currentLvl.width}×${currentLvl.height}` : video.videoWidth ? `${video.videoWidth}×${video.videoHeight}` : '—',
        latency: diagnostics.latency,
        level: hls ? hls.currentLevel : 0,
        totalLevels: hls ? hls.levels.length : 1,
      });
    }, 500);

    return () => clearInterval(interval);
  }, [showDiagnostics, diagnostics.latency]);

  // ── 4. Video Events & Listeners ──
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

  // ── 5. Keyboard Navigation & Short-cuts ──
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
        case 'p':
          e.preventDefault();
          togglePip();
          break;
        case 'arrowleft':
          e.preventDefault();
          seekRelative(-5);
          break;
        case 'arrowright':
          e.preventDefault();
          seekRelative(5);
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
        case '?':
        case 'h':
          e.preventDefault();
          setShowHelpModal(prev => !prev);
          break;
        default:
          // 0-9 seek percent
          if (/^[0-9]$/.test(e.key)) {
            e.preventDefault();
            const pct = parseInt(e.key, 10) * 0.1;
            video.currentTime = pct * video.duration;
          }
          break;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  // ── 6. Fullscreen & PiP Detection ──
  useEffect(() => {
    const fsHandler = () => setIsFullscreen(!!document.fullscreenElement);
    const pipHandler = () => setIsPip(!!document.pictureInPictureElement);

    document.addEventListener('fullscreenchange', fsHandler);
    document.addEventListener('enterpictureinpicture', pipHandler);
    document.addEventListener('leavepictureinpicture', pipHandler);

    return () => {
      document.removeEventListener('fullscreenchange', fsHandler);
      document.removeEventListener('enterpictureinpicture', pipHandler);
      document.removeEventListener('leavepictureinpicture', pipHandler);
    };
  }, []);

  // ── 7. Controls Auto-Hide Timer ──
  const showControlsTemporarily = useCallback(() => {
    setShowControls(true);
    clearTimeout(controlsTimeoutRef.current);
    if (!showControlsOnHover) return; // Keep visible if showControlsOnHover is false
    controlsTimeoutRef.current = window.setTimeout(() => {
      if (videoRef.current && !videoRef.current.paused) {
        setShowControls(false);
      }
    }, controlsAutoHideMs);
  }, [showControlsOnHover, controlsAutoHideMs]);

  // ── Helper Actions ──
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
      document.exitFullscreen().catch(() => {});
    } else {
      container.requestFullscreen().catch(() => {});
    }
  };

  const togglePip = async () => {
    const video = videoRef.current;
    if (!video || !document.pictureInPictureEnabled) return;
    try {
      if (document.pictureInPictureElement) {
        await document.exitPictureInPicture();
      } else {
        await video.requestPictureInPicture();
      }
    } catch (err) {
      console.warn('PiP failed:', err);
    }
  };

  const seekRelative = (deltaSec: number) => {
    const video = videoRef.current;
    if (!video) return;
    video.currentTime = Math.max(0, Math.min(video.duration, video.currentTime + deltaSec));
    setSeekRipple({
      type: deltaSec < 0 ? 'rewind' : 'forward',
      id: Date.now()
    });
    setTimeout(() => setSeekRipple(null), 600);
  };

  const handleSpeedChange = (rate: number) => {
    const video = videoRef.current;
    if (!video) return;
    video.playbackRate = rate;
    setPlaybackRate(rate);
    setShowSpeedMenu(false);
  };

  const setQualityLevel = (level: number) => {
    const hls = hlsRef.current;
    if (!hls) return;
    hls.currentLevel = level; // -1 = auto
    setCurrentLevel(level);
    setShowQualityMenu(false);
  };

  // ── Progress Bar Hover & Sprite Thumbnail Math ──
  const handleProgressMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
    const bar = progressRef.current;
    if (!bar || duration <= 0) return;
    const rect = bar.getBoundingClientRect();
    const x = Math.max(0, Math.min(e.clientX - rect.left, rect.width));
    const percent = x / rect.width;
    const hoverTime = percent * duration;

    setHoverState({
      visible: true,
      time: hoverTime,
      percent: percent * 100,
      xPx: x,
    });
  };

  const handleProgressMouseLeave = () => {
    setHoverState(prev => ({ ...prev, visible: false }));
  };

  const seekTo = (e: React.MouseEvent<HTMLDivElement>) => {
    const video = videoRef.current;
    const bar = progressRef.current;
    if (!video || !bar || duration <= 0) return;
    const rect = bar.getBoundingClientRect();
    const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    video.currentTime = pct * video.duration;
  };

  // Calculate current Sprite Style for preview scrubbing
  const currentSpriteStyle = useMemo(() => {
    if (!hoverState.visible || duration <= 0) return null;

    // 1. Try VTT Cues first
    if (vttCues.length > 0) {
      const cue = vttCues.find(c => hoverState.time >= c.startTime && hoverState.time <= c.endTime);
      if (cue) {
        return {
          backgroundImage: `url(${cue.url})`,
          backgroundPosition: `-${cue.x}px -${cue.y}px`,
          width: `${cue.w}px`,
          height: `${cue.h}px`,
        };
      }
    }

    // 2. Fallback to direct Sprite Grid Config if spriteUrl is provided
    if (spriteUrl && !spriteUrl.endsWith('.vtt')) {
      const interval = spriteConfig.intervalSec || 5;
      const frameIdx = Math.floor(hoverState.time / interval);
      const cols = spriteConfig.cols || 10;
      const row = Math.floor(frameIdx / cols);
      const col = frameIdx % cols;
      const x = col * spriteConfig.width;
      const y = row * spriteConfig.height;

      return {
        backgroundImage: `url(${spriteUrl})`,
        backgroundPosition: `-${x}px -${y}px`,
        width: `${spriteConfig.width}px`,
        height: `${spriteConfig.height}px`,
      };
    }

    return null;
  }, [hoverState.visible, hoverState.time, duration, vttCues, spriteUrl, spriteConfig]);

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
  const isLight = theme === 'light';
  const resolvedControlsBg = controlsBg === '#000000' && isLight ? '#ffffff' : controlsBg;
  const resolvedIconColor = controlsIconColor === '#ffffff' && isLight ? '#27272a' : controlsIconColor; // zinc-800
  const resolvedAccentColor = accentColor === '#ffffff' && isLight ? '#000000' : accentColor;

  const showOverlayControls = isFullscreen || controlsPosition === "overlay";
  const opacityHex = Math.round(controlsBgOpacity * 2.55).toString(16).padStart(2, '0');

  const controlBarContent = (
    <div 
      className={cn(
        showOverlayControls
          ? "absolute bottom-0 inset-x-0 px-4 pb-4 pt-16 flex flex-col gap-3 transition-all duration-300 z-20"
          : "w-full px-4 py-3 flex flex-col gap-3 border-t",
        showOverlayControls && (showControls ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2 pointer-events-none"),
        !showOverlayControls && (isLight ? "bg-white border-gray-200" : "bg-neutral-950 border-neutral-900"),
        classNames.controlBar
      )}
      style={{
        background: showOverlayControls
          ? `linear-gradient(to top, ${resolvedControlsBg}${opacityHex} 0%, ${resolvedControlsBg}${opacityHex} 75%, transparent 100%)`
          : resolvedControlsBg,
        color: resolvedIconColor
      }}
    >
      {/* Progress Bar Container with Preview Sprite Scrubbing Tooltip */}
      <div className="relative w-full">
        {/* Floating Sprite Preview Scrubbing Tooltip */}
        {hoverState.visible && (
          <div 
            className={cn(
              "absolute bottom-4 -translate-x-1/2 flex flex-col items-center border rounded p-1.5 shadow-2xl pointer-events-none z-30 transition-all duration-75",
              isLight ? "bg-white/95 border-gray-200 text-gray-800" : "bg-neutral-950/95 border-neutral-800 text-neutral-300",
              classNames.previewTooltip
            )}
            style={{ left: `${hoverState.xPx}px` }}
          >
            {currentSpriteStyle ? (
              <div 
                className={cn("rounded border overflow-hidden bg-black mb-1 bg-no-repeat", isLight ? "border-gray-200" : "border-neutral-800")}
                style={currentSpriteStyle}
              />
            ) : (
              <div className={cn("w-28 h-16 rounded border flex items-center justify-center text-[9px] font-mono mb-1", isLight ? "bg-gray-100 border-gray-200 text-gray-400" : "bg-neutral-900 border-neutral-800 text-neutral-500")}>
                NO PREVIEW
              </div>
            )}
            <span className={cn("text-[10px] font-mono font-bold px-2 py-0.5 rounded border", isLight ? "bg-gray-100 border-gray-200 text-gray-800" : "bg-neutral-900 border-neutral-800 text-white")}>
              {formatTime(hoverState.time)}
            </span>
          </div>
        )}

        {/* Seek Progress Line */}
        <div 
          className={cn(
            "relative bg-neutral-800/80 rounded-full cursor-pointer transition-all duration-150 group/progress",
            classNames.progressBarContainer
          )}
          style={{ height: `${progressBarHeight}px` }}
          ref={progressRef} 
          onMouseMove={handleProgressMouseMove}
          onMouseLeave={handleProgressMouseLeave}
          onClick={seekTo}
        >
          {/* Buffered Track */}
          <div className="absolute inset-y-0 left-0 bg-neutral-700/60 rounded-full" style={{ width: `${bufferedPct}%` }} />
          
          {/* Hover Track Indicator */}
          {hoverState.visible && (
            <div 
              className="absolute inset-y-0 left-0 bg-white/20 rounded-full pointer-events-none"
              style={{ width: `${hoverState.percent}%` }}
            />
          )}

          {/* Played Track */}
          <div 
            className={cn("absolute inset-y-0 left-0 rounded-full flex items-center justify-end", classNames.progressBar)}
            style={{ width: `${progressPct}%`, backgroundColor: accentColor }}
          >
            <div 
              className="w-3 h-3 rounded-full shadow-md scale-0 group-hover/progress:scale-100 transition-transform duration-150 translate-x-1.5" 
              style={{ backgroundColor: accentColor }}
            />
          </div>
        </div>
      </div>

      {/* Control Buttons Bar */}
      <div className="flex justify-between items-center">
        {/* Left Controls */}
        <div className="flex items-center gap-3">
          <button 
            className={cn(
              "p-1.5 transition-colors rounded",
              isLight ? "hover:bg-black/5" : "hover:bg-white/10",
              classNames.controlButtonLeft
            )}
            onClick={togglePlay} 
            aria-label={isPlaying ? 'Pause' : 'Play'}
            style={{ color: resolvedIconColor }}
          >
            {isPlaying ? <Pause className="h-4 w-4 fill-current" /> : <Play className="h-4 w-4 fill-current" />}
          </button>

          {/* Volume Control */}
          <div className="flex items-center gap-1.5 group/volume">
            <button 
              className={cn(
                "p-1.5 transition-colors rounded",
                isLight ? "hover:bg-black/5" : "hover:bg-white/10",
                classNames.controlButtonLeft
              )}
              onClick={toggleMute} 
              aria-label={isMuted ? 'Unmute' : 'Mute'}
              style={{ color: resolvedIconColor }}
            >
              {isMuted || volume === 0 ? <VolumeX className="h-4 w-4" /> : <Volume2 className="h-4 w-4" />}
            </button>
            {showVolumeSlider && (
              <input
                type="range"
                className={cn(
                  "w-0 opacity-0 group-hover/volume:w-16 group-hover/volume:opacity-100 transition-all duration-200 h-1 bg-neutral-800 rounded-full appearance-none cursor-pointer",
                  classNames.volumeSlider
                )}
                style={{ accentColor: resolvedAccentColor }}
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
            )}
          </div>

          {showTimeDisplay && (
            <span className={cn("text-[11px] font-mono select-none", isLight ? "text-gray-500" : "text-neutral-400", classNames.timeDisplay)}>
              {formatTime(currentTime)} / {formatTime(duration)}
            </span>
          )}
        </div>

        {/* Right Controls */}
        <div className="flex items-center gap-2.5">
          {/* Speed Selector */}
          {showSpeedSelector && (
            <div className="relative">
              <button
                className={cn(
                  "flex items-center gap-1 px-2 py-1 text-[10px] font-mono rounded border transition-colors",
                  isLight ? "border-gray-300 hover:bg-gray-150" : "border-neutral-800 hover:bg-neutral-900",
                  showSpeedMenu && (isLight ? "border-gray-400 bg-gray-100" : "border-neutral-700 bg-neutral-900"),
                  classNames.speedSelectorButton
                )}
                style={{ color: resolvedIconColor }}
                onClick={() => { setShowSpeedMenu(prev => !prev); setShowQualityMenu(false); }}
                aria-label="Playback speed"
              >
                <Gauge className="h-3 w-3" />
                {playbackRate}x
              </button>
              {showSpeedMenu && (
                <div className={cn(
                  "absolute bottom-8 right-0 border rounded p-1 shadow-2xl w-24 flex flex-col gap-0.5 z-40 animate-in fade-in duration-100",
                  isLight ? "bg-white border-gray-200" : "bg-neutral-950 border-neutral-800",
                  classNames.speedSelectorMenu
                )}>
                  {[0.5, 0.75, 1, 1.25, 1.5, 2].map((rate) => (
                    <button
                      key={rate}
                      className={cn(
                        "w-full text-left px-2 py-1 text-[10px] font-mono rounded flex justify-between items-center transition-colors",
                        isLight ? "text-gray-600 hover:bg-gray-100 hover:text-black" : "text-neutral-400 hover:bg-neutral-900 hover:text-white",
                        playbackRate === rate && (isLight ? "text-black font-semibold bg-gray-100" : "text-white font-semibold bg-neutral-900")
                      )}
                      onClick={() => handleSpeedChange(rate)}
                    >
                      <span>{rate}x</span>
                      {playbackRate === rate && <span className={isLight ? "text-black text-[8px]" : "text-white text-[8px]"}>●</span>}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Quality Selector */}
          {showQualitySelector && (
            <div className="relative">
              <button
                className={cn(
                  "flex items-center gap-1 px-2 py-1 text-[10px] font-mono rounded border transition-colors",
                  isLight ? "border-gray-300 hover:bg-gray-150" : "border-neutral-800 hover:bg-neutral-900",
                  showQualityMenu && (isLight ? "border-gray-400 bg-gray-100" : "border-neutral-700 bg-neutral-900"),
                  classNames.qualitySelectorButton
                )}
                style={{ color: resolvedIconColor }}
                onClick={() => { setShowQualityMenu(prev => !prev); setShowSpeedMenu(false); }}
                aria-label="Quality"
              >
                <SlidersHorizontal className="h-3 w-3" />
                {getQualityLabel(currentLevel)}
              </button>
              {showQualityMenu && (
                <div className={cn(
                  "absolute bottom-8 right-0 border rounded p-1 shadow-2xl w-28 flex flex-col gap-0.5 z-40 animate-in fade-in duration-100",
                  isLight ? "bg-white border-gray-200" : "bg-neutral-950 border-neutral-800",
                  classNames.qualitySelectorMenu
                )}>
                  <button
                    className={cn(
                      "w-full text-left px-2 py-1.5 text-[10px] font-mono rounded flex justify-between items-center transition-colors",
                      isLight ? "text-gray-600 hover:bg-gray-100 hover:text-black" : "text-neutral-400 hover:bg-neutral-900 hover:text-white",
                      currentLevel === -1 && (isLight ? "text-black font-semibold bg-gray-100" : "text-white font-semibold bg-neutral-900")
                    )}
                    onClick={() => setQualityLevel(-1)}
                  >
                    Auto
                  </button>
                  {levels.map((lvl, i) => (
                    <button
                      key={i}
                      className={cn(
                        "w-full text-left px-2 py-1.5 text-[10px] font-mono rounded flex flex-col gap-0.5 transition-colors",
                        isLight ? "text-gray-600 hover:bg-gray-100 hover:text-black" : "text-neutral-400 hover:bg-neutral-900 hover:text-white",
                        currentLevel === i && (isLight ? "text-black font-semibold bg-gray-100" : "text-white font-semibold bg-neutral-900")
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
          )}

          {/* Toggle Diagnostics */}
          {showDiagnosticsButton && (
            <button
              className={cn(
                "p-1.5 rounded transition-colors",
                isLight ? "hover:bg-black/5" : "hover:bg-white/10",
                showDiagnostics ? (isLight ? "text-black bg-gray-100" : "text-white bg-neutral-900") : (isLight ? "text-gray-500" : "text-neutral-400")
              )}
              onClick={() => setShowDiagnostics(prev => !prev)}
              aria-label="Toggle diagnostics"
              style={{ color: resolvedIconColor }}
            >
              <Activity className="h-4 w-4" />
            </button>
          )}

          {/* Picture in Picture */}
          {showPipButton && isPipSupported && (
            <button 
              className={cn(
                "p-1.5 transition-colors rounded",
                isLight ? "hover:bg-black/5" : "hover:bg-white/10",
                isPip && (isLight ? "text-black bg-gray-100" : "text-white bg-neutral-900"),
                classNames.pipButton
              )} 
              onClick={togglePip} 
              aria-label="Picture in Picture"
              style={{ color: resolvedIconColor }}
            >
              <PictureInPicture2 className="h-4 w-4" />
            </button>
          )}

          {/* Toggle Keyboard Shortcuts Modal */}
          {showHelpButton && (
            <button
              className={cn(
                "p-1.5 rounded transition-colors",
                isLight ? "hover:bg-black/5" : "hover:bg-white/10",
                showHelpModal ? (isLight ? "text-black bg-gray-100" : "text-white bg-neutral-900") : (isLight ? "text-gray-500" : "text-neutral-400")
              )}
              onClick={() => setShowHelpModal(prev => !prev)}
              aria-label="Keyboard Shortcuts"
              title="Keyboard Shortcuts (?)"
              style={{ color: resolvedIconColor }}
            >
              <HelpCircle className="h-4 w-4" />
            </button>
          )}

          {/* Toggle Fullscreen */}
          <button 
            className={cn(
              "p-1.5 transition-colors rounded",
              isLight ? "hover:bg-black/5" : "hover:bg-white/10",
              classNames.fullscreenButton
            )} 
            onClick={toggleFullscreen} 
            aria-label={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
            style={{ color: resolvedIconColor }}
          >
            {isFullscreen ? <Minimize className="h-4 w-4" /> : <Maximize className="h-4 w-4" />}
          </button>
        </div>
      </div>
    </div>
  );

  return (
    <div
      className={cn(
        "relative flex flex-col group select-none transition-all duration-300 border overflow-hidden shadow-2xl",
        isLight ? "bg-white text-gray-800 border-gray-200" : "bg-black text-neutral-100 border-neutral-800",
        className,
        classNames.container
      )}
      style={{
        borderRadius: isFullscreen ? '0' : `${borderRadius}px`,
        width: '100%',
        aspectRatio: (showOverlayControls && !isFullscreen) ? aspectRatio : undefined,
      }}
      ref={containerRef}
      onMouseMove={showControlsTemporarily}
      onMouseLeave={() => { if (isPlaying) setShowControls(false); }}
    >
      {/* Video Viewport Wrapper */}
      <div 
        className={cn(
          "relative w-full bg-black overflow-hidden flex items-center justify-center",
          showOverlayControls ? "h-full w-full" : "aspect-video"
        )}
        style={{
          aspectRatio: showOverlayControls ? undefined : aspectRatio,
          borderRadius: (showOverlayControls || isFullscreen) ? '0' : `${borderRadius}px ${borderRadius}px 0 0`,
        }}
      >
        <video
          ref={videoRef}
          className={cn("w-full h-full object-cover cursor-pointer", classNames.video)}
          poster={poster}
          onClick={togglePlay}
          onDoubleClick={toggleFullscreen}
          playsInline
        />

        {/* Ripple Feedback Animation for Seek (-5s / +5s) */}
        {showSeekRipple && seekRipple && (
          <div className={cn(
            "absolute top-1/2 -translate-y-1/2 flex items-center justify-center p-6 bg-black/60 rounded-full text-white backdrop-blur border border-white/20 animate-in fade-in zoom-in-75 duration-200 pointer-events-none z-30",
            seekRipple.type === 'rewind' ? "left-12" : "right-12",
            classNames.seekRipple
          )}>
            {seekRipple.type === 'rewind' ? (
              <div className="flex flex-col items-center gap-1">
                <RotateCcw className="h-8 w-8 animate-spin" />
                <span className="font-mono text-xs font-bold">-5s</span>
              </div>
            ) : (
              <div className="flex flex-col items-center gap-1">
                <RotateCw className="h-8 w-8 animate-spin" />
                <span className="font-mono text-xs font-bold">+5s</span>
              </div>
            )}
          </div>
        )}

        {/* Play Overlay (shown when paused) */}
        {showPlayOverlay && !isPlaying && (
          <div 
            className={cn(
              "absolute inset-0 flex items-center justify-center bg-black/50 cursor-pointer transition-opacity duration-300 z-10",
              classNames.playOverlay
            )}
            onClick={togglePlay}
          >
            <button className={cn("rounded-full bg-white p-5 text-black transition-transform duration-200 hover:scale-110 active:scale-95 shadow-2xl border border-white/40", classNames.playButton)}>
              <Play className="h-7 w-7 fill-current translate-x-0.5" />
            </button>
          </div>
        )}

        {/* Buffering Spinner Overlay */}
        {isBuffering && isPlaying && (
          <div className={cn("absolute inset-0 flex items-center justify-center bg-black/60 pointer-events-none z-10", classNames.bufferingOverlay)}>
            <div className="h-10 w-10 rounded-full border-2 border-neutral-700 border-t-white animate-spin" />
          </div>
        )}

        {/* Diagnostics Overlay */}
        {showDiagnostics && (
          <div className={cn(
            "absolute top-4 left-4 border rounded-lg p-4 w-64 text-[10px] font-mono z-20 space-y-1.5 pointer-events-none shadow-2xl backdrop-blur",
            isLight ? "bg-white/95 border-gray-200 text-gray-800" : "bg-neutral-950/90 border-neutral-800 text-neutral-300",
            classNames.diagnosticsPanel
          )}>
            <div className={cn("font-bold border-b pb-1.5 mb-2 flex items-center justify-between", isLight ? "text-gray-900 border-gray-200" : "text-white border-neutral-800")}>
              <span className="flex items-center gap-1.5">
                <Activity className={cn("h-3.5 w-3.5", isLight ? "text-black" : "text-white")} /> STREAM TELEMETRY
              </span>
              <span className="text-[9px] text-zinc-500 font-normal">REAL-TIME</span>
            </div>
            <div className={cn("flex justify-between border-b pb-1", isLight ? "border-gray-100" : "border-neutral-900")}>
              <span>Resolution</span><span className={cn("font-semibold", isLight ? "text-black" : "text-white")}>{diagnostics.resolution}</span>
            </div>
            <div className={cn("flex justify-between border-b pb-1", isLight ? "border-gray-100" : "border-neutral-900")}>
              <span>Bitrate</span><span className={cn("font-semibold", isLight ? "text-black" : "text-white")}>{diagnostics.bitrate} Kbps</span>
            </div>
            <div className={cn("flex justify-between border-b pb-1", isLight ? "border-gray-100" : "border-neutral-900")}>
              <span>Buffer Occupancy</span><span className={cn("font-semibold", isLight ? "text-black" : "text-white")}>{diagnostics.bufferLength}s</span>
            </div>
            <div className={cn("flex justify-between border-b pb-1", isLight ? "border-gray-100" : "border-neutral-900")}>
              <span>Dropped Frames</span><span className={cn("font-semibold", isLight ? "text-black" : "text-white")}>{diagnostics.droppedFrames}</span>
            </div>
            <div className={cn("flex justify-between border-b pb-1", isLight ? "border-gray-100" : "border-neutral-900")}>
              <span>Segment Latency</span><span className={cn("font-semibold", isLight ? "text-black" : "text-white")}>{diagnostics.latency}ms</span>
            </div>
            <div className="flex justify-between">
              <span>ABR Level</span><span className={cn("font-semibold", isLight ? "text-black" : "text-white")}>{diagnostics.level + 1}/{diagnostics.totalLevels}</span>
            </div>
          </div>
        )}

        {/* Overlay Controls */}
        {showOverlayControls && controlBarContent}
      </div>

      {/* Below Controls */}
      {!showOverlayControls && controlBarContent}

      {/* Keyboard Shortcuts Help Modal Overlay */}
      {showHelpModal && (
        <div className="absolute inset-0 bg-black/80 backdrop-blur-md z-50 flex items-center justify-center p-6 animate-in fade-in duration-150">
          <div className={cn(
            "border rounded-xl p-6 w-full max-w-sm flex flex-col gap-4 text-xs font-mono shadow-2xl relative",
            isLight ? "bg-white border-gray-250 text-gray-800" : "bg-neutral-950 border-neutral-800 text-neutral-300",
            classNames.helpModal
          )}>
            <div className={cn("flex justify-between items-center border-b pb-3", isLight ? "border-gray-150" : "border-neutral-900")}>
              <span className={cn("font-bold uppercase tracking-wider flex items-center gap-2", isLight ? "text-gray-900" : "text-white")}>
                <HelpCircle className="h-4 w-4" /> KEYBOARD SHORTCUTS
              </span>
              <button 
                onClick={() => setShowHelpModal(false)}
                className={cn(
                  "p-1 rounded transition-colors",
                  isLight ? "text-gray-500 hover:text-black hover:bg-gray-100" : "text-neutral-400 hover:text-white hover:bg-neutral-900"
                )}
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="grid grid-cols-2 gap-y-2 text-[10px]">
              <div className="flex items-center gap-2"><kbd className={cn("px-1.5 py-0.5 rounded border font-bold", isLight ? "bg-gray-100 border-gray-250 text-black" : "bg-neutral-900 border-neutral-800 text-white")}>Space / K</kbd></div>
              <div>Play / Pause</div>

              <div className="flex items-center gap-2"><kbd className={cn("px-1.5 py-0.5 rounded border font-bold", isLight ? "bg-gray-100 border-gray-250 text-black" : "bg-neutral-900 border-neutral-800 text-white")}>F</kbd></div>
              <div>Toggle Fullscreen</div>

              <div className="flex items-center gap-2"><kbd className={cn("px-1.5 py-0.5 rounded border font-bold", isLight ? "bg-gray-100 border-gray-250 text-black" : "bg-neutral-900 border-neutral-800 text-white")}>M</kbd></div>
              <div>Mute / Unmute Audio</div>

              <div className="flex items-center gap-2"><kbd className={cn("px-1.5 py-0.5 rounded border font-bold", isLight ? "bg-gray-100 border-gray-250 text-black" : "bg-neutral-900 border-neutral-800 text-white")}>P</kbd></div>
              <div>Picture-in-Picture</div>

              <div className="flex items-center gap-2"><kbd className={cn("px-1.5 py-0.5 rounded border font-bold", isLight ? "bg-gray-100 border-gray-250 text-black" : "bg-neutral-900 border-neutral-800 text-white")}>D</kbd></div>
              <div>Stream Telemetry</div>

              <div className="flex items-center gap-2"><kbd className={cn("px-1.5 py-0.5 rounded border font-bold", isLight ? "bg-gray-100 border-gray-250 text-black" : "bg-neutral-900 border-neutral-800 text-white")}>← / →</kbd></div>
              <div>Seek -5s / +5s</div>

              <div className="flex items-center gap-2"><kbd className={cn("px-1.5 py-0.5 rounded border font-bold", isLight ? "bg-gray-100 border-gray-250 text-black" : "bg-neutral-900 border-neutral-800 text-white")}>↑ / ↓</kbd></div>
              <div>Volume +10% / -10%</div>

              <div className="flex items-center gap-2"><kbd className={cn("px-1.5 py-0.5 rounded border font-bold", isLight ? "bg-gray-100 border-gray-250 text-black" : "bg-neutral-900 border-neutral-800 text-white")}>0 - 9</kbd></div>
              <div>Seek 0% - 90%</div>
            </div>

            <div className={cn("text-[9px] text-center pt-2 border-t uppercase", isLight ? "border-gray-150 text-gray-400" : "border-neutral-900 text-neutral-500")}>
              Press <kbd className="px-1 bg-neutral-900 border border-neutral-850 rounded">?</kbd> or <kbd className="px-1 bg-neutral-900 border border-neutral-850 rounded">Esc</kbd> to close
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
