import React, { useState, useRef, useEffect, useMemo } from 'react';
import { 
  MoreVertical, 
  CheckCircle2, 
  Clock, 
  VolumeX, 
  Volume2 
} from 'lucide-react';
import Hls from 'hls.js';
import { cn } from '../lib/utils';
import { parseVTTCues, generateSpriteCuesForDuration, createDemoPosterDataUrl, type SpriteCue } from '../lib/vtt';

export interface VideoTileClassNames {
  root?: string;
  thumbnail?: string;
  poster?: string;
  spritePreview?: string;
  videoPreview?: string;
  badge?: string;
  duration?: string;
  progressBar?: string;
  progressFill?: string;
  metadata?: string;
  avatar?: string;
  title?: string;
  channelName?: string;
  viewsRow?: string;
  menuButton?: string;
  muteButton?: string;
}

export interface VideoTileProps {
  id?: string;
  gatewayUrl?: string;
  jobId?: string;
  title: string;
  channelName: string;
  channelAvatar?: string;
  views?: string;
  uploadedAt?: string;
  duration?: string;
  posterUrl?: string;
  spriteUrl?: string;
  spriteVttUrl?: string;
  previewFrames?: string[];
  previewVideoUrl?: string;
  badge?: string;
  isVerified?: boolean;
  onClick?: () => void;
  onMenuClick?: (e: React.MouseEvent) => void;
  className?: string;
  classNames?: VideoTileClassNames;

  // Visual Customization
  hoverScale?: number;          // default 1.02
  hoverDelayMs?: number;        // default 400
  flipbookIntervalMs?: number;  // default 350
  borderRadius?: number;        // px, default 12
  aspectRatio?: string;         // default "16/9"

  // Visibility Toggles
  showBadge?: boolean;          // default true
  showDuration?: boolean;       // default true
  showProgressBar?: boolean;    // default true
  showAvatar?: boolean;         // default true
  showVerified?: boolean;       // default true

  // Typography
  titleLines?: 1 | 2 | 3;      // default 2

  // Theme
  theme?: 'dark' | 'light';    // default 'dark'
}

const DEFAULT_PREVIEW_FRAMES = [
  createDemoPosterDataUrl('Preview Keyframe 1 (0:00)', 210),
  createDemoPosterDataUrl('Preview Keyframe 2 (0:03)', 250),
  createDemoPosterDataUrl('Preview Keyframe 3 (0:06)', 290),
  createDemoPosterDataUrl('Preview Keyframe 4 (0:09)', 330),
  createDemoPosterDataUrl('Preview Keyframe 5 (0:12)', 30),
];

export const VideoTile: React.FC<VideoTileProps> = ({
  gatewayUrl,
  jobId,
  title,
  channelName,
  channelAvatar,
  views = '124K views',
  uploadedAt = '3 days ago',
  duration = '10:15',
  posterUrl: initialPosterUrl,
  spriteUrl,
  spriteVttUrl: initialSpriteVttUrl,
  previewFrames,
  previewVideoUrl: initialPreviewVideoUrl,
  badge = '4K',
  isVerified = true,
  onClick,
  onMenuClick,
  className,
  classNames = {},
  hoverScale = 1.02,
  hoverDelayMs = 400,
  flipbookIntervalMs = 350,
  borderRadius = 12,
  aspectRatio = '16/9',
  showBadge = true,
  showDuration = true,
  showProgressBar = true,
  showAvatar = true,
  showVerified = true,
  titleLines = 2,
  theme = 'dark',
}) => {
  // Resolve Backend URLs if gatewayUrl and jobId are passed
  const posterUrl = initialPosterUrl || (gatewayUrl && jobId ? `${gatewayUrl}/storage/jobs/${jobId}/poster.jpg` : createDemoPosterDataUrl(title));
  const spriteVttUrl = initialSpriteVttUrl || (gatewayUrl && jobId ? `${gatewayUrl}/storage/jobs/${jobId}/sprite.vtt` : undefined);
  const previewVideoUrl = initialPreviewVideoUrl || (gatewayUrl && jobId ? `${gatewayUrl}/storage/jobs/${jobId}/preview.mp4` : undefined);
  const [isHovered, setIsHovered] = useState(false);
  const [vttCues, setVttCues] = useState<SpriteCue[]>([]);
  const [activeCueIndex, setActiveCueIndex] = useState(0);
  const [isVideoPlaying, setIsVideoPlaying] = useState(false);
  const [isMuted, setIsMuted] = useState(true);

  // Video time tracking for duration decrease/progress bar in video mode
  const [previewTime, setPreviewTime] = useState<number>(0);
  const [previewDuration, setPreviewDuration] = useState<number>(0);

  const hoverTimerRef = useRef<number>(0);
  const flipbookIntervalRef = useRef<number>(0);
  const videoRef = useRef<HTMLVideoElement>(null);

  // 1. Parse or Generate Sprite Cues
  useEffect(() => {
    if (spriteUrl && !spriteUrl.endsWith('.vtt') && !spriteVttUrl) {
      const generated = generateSpriteCuesForDuration(100, 20, spriteUrl);
      setVttCues(generated);
      return;
    }

    const targetVtt = spriteVttUrl || (spriteUrl && spriteUrl.endsWith('.vtt') ? spriteUrl : undefined);
    if (!targetVtt) {
      setVttCues([]);
      return;
    }

    let isSubscribed = true;
    fetch(targetVtt)
      .then(res => res.ok ? res.text() : '')
      .then(text => {
        if (!isSubscribed || !text) return;
        const parsed = parseVTTCues(text, targetVtt);
        setVttCues(parsed);
      })
      .catch(err => console.warn('Failed to parse tile VTT:', err));

    return () => { isSubscribed = false; };
  }, [spriteUrl, spriteVttUrl]);

  // Attach HLS stream to video element if previewVideoUrl is HLS stream
  useEffect(() => {
    if (!previewVideoUrl || !videoRef.current) return;

    let hls: Hls | null = null;
    const video = videoRef.current;

    if (previewVideoUrl.includes('.m3u8')) {
      if (Hls.isSupported()) {
        hls = new Hls({ autoStartLoad: true });
        hls.loadSource(previewVideoUrl);
        hls.attachMedia(video);
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = previewVideoUrl;
      }
    } else {
      video.src = previewVideoUrl;
    }

    return () => {
      if (hls) hls.destroy();
    };
  }, [previewVideoUrl]);

  // Handle timeupdate and loadedmetadata for preview video to calculate descending timer & progress
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !previewVideoUrl) return;

    const handleTimeUpdate = () => {
      setPreviewTime(video.currentTime);
    };

    const handleLoadedMetadata = () => {
      setPreviewDuration(video.duration);
    };

    video.addEventListener('timeupdate', handleTimeUpdate);
    video.addEventListener('loadedmetadata', handleLoadedMetadata);

    // Initial check
    if (video.duration) {
      setPreviewDuration(video.duration);
    }

    return () => {
      video.removeEventListener('timeupdate', handleTimeUpdate);
      video.removeEventListener('loadedmetadata', handleLoadedMetadata);
    };
  }, [previewVideoUrl, isVideoPlaying]);

  // Unmount Timer Cleanup
  useEffect(() => {
    return () => {
      clearTimeout(hoverTimerRef.current);
      clearInterval(flipbookIntervalRef.current);
    };
  }, []);

  // Theme-derived colors
  const isLight = theme === 'light';
  const titleLineClamp = titleLines === 1 ? 'line-clamp-1' : titleLines === 3 ? 'line-clamp-3' : 'line-clamp-2';

  // Descending time math for muted video preview mode
  const totalDurationSec = previewDuration || parseDurationToSeconds(duration);
  const remainingSeconds = Math.max(0, totalDurationSec - previewTime);
  const isVideoPlayingMode = !!(previewVideoUrl && isVideoPlaying);
  const durationText = isVideoPlayingMode 
    ? `-${formatSeconds(remainingSeconds)}` 
    : duration;

  // 2. Handle Mouse Enter with configurable delay
  const handleMouseEnter = () => {
    clearTimeout(hoverTimerRef.current);
    hoverTimerRef.current = window.setTimeout(() => {
      setIsHovered(true);
    }, hoverDelayMs);
  };

  const handleMouseLeave = () => {
    clearTimeout(hoverTimerRef.current);
    clearInterval(flipbookIntervalRef.current);
    setIsHovered(false);
    setActiveCueIndex(0);
    setIsVideoPlaying(false);
    if (videoRef.current) {
      videoRef.current.pause();
      videoRef.current.currentTime = 0;
    }
  };

  // 3. Sprite Flipbook Cycling Animation or Muted Video Playback
  const activeFrames = previewFrames || DEFAULT_PREVIEW_FRAMES;

  useEffect(() => {
    if (!isHovered) {
      clearInterval(flipbookIntervalRef.current);
      return;
    }

    if (previewVideoUrl && !spriteUrl) {
      if (videoRef.current) {
        videoRef.current.play().then(() => setIsVideoPlaying(true)).catch(() => {});
      }
    } else if (vttCues.length > 0) {
      // Sequence through WebVTT cues at configurable interval
      flipbookIntervalRef.current = window.setInterval(() => {
        setActiveCueIndex(prev => (prev + 1) % vttCues.length);
      }, flipbookIntervalMs);
    } else {
      // Fallback keyframe image sequence flipbook
      flipbookIntervalRef.current = window.setInterval(() => {
        setActiveCueIndex(prev => (prev + 1) % activeFrames.length);
      }, flipbookIntervalMs);
    }

    return () => clearInterval(flipbookIntervalRef.current);
  }, [isHovered, vttCues, activeFrames, previewVideoUrl, spriteUrl, flipbookIntervalMs]);

  // Active Sprite Style or Fallback Image Frame
  const activeFrameUrl = useMemo(() => {
    if (!isHovered) return null;
    if (vttCues.length > 0) return null;
    return activeFrames[activeCueIndex % activeFrames.length];
  }, [isHovered, vttCues, activeFrames, activeCueIndex]);

  const currentSpriteStyle = useMemo(() => {
    if (!isHovered || vttCues.length === 0) return null;
    const cue = vttCues[activeCueIndex];
    if (!cue) return null;

    // Derive total grid from all cues (max x+w = sheet width, max y+h = sheet height)
    let sheetW = cue.w, sheetH = cue.h;
    for (const c of vttCues) {
      sheetW = Math.max(sheetW, c.x + c.w);
      sheetH = Math.max(sheetH, c.y + c.h);
    }
    const totalCols = Math.round(sheetW / cue.w) || 1;
    const totalRows = Math.round(sheetH / cue.h) || 1;
    const col = Math.round(cue.x / cue.w);
    const row = Math.round(cue.y / cue.h);

    return {
      backgroundImage: `url(${cue.url})`,
      backgroundSize: `${totalCols * 100}% ${totalRows * 100}%`,
      backgroundPosition: `${totalCols > 1 ? col * (100 / (totalCols - 1)) : 0}% ${totalRows > 1 ? row * (100 / (totalRows - 1)) : 0}%`,
    };
  }, [isHovered, vttCues, activeCueIndex]);

  // Progress percentage during hover
  const hoverProgressPct = vttCues.length > 0 
    ? ((activeCueIndex + 1) / vttCues.length) * 100 
    : 0;

  return (
    <div 
      className={cn(
        "group relative flex flex-col gap-3 overflow-hidden cursor-pointer select-none transition-all duration-300",
        isLight ? 'bg-white' : '',
        className,
        classNames.root
      )}
      style={{ 
        borderRadius: `${borderRadius}px`,
        ['--hover-scale' as string]: hoverScale,
      }}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      onClick={onClick}
    >
      {/* Thumbnail / Preview Player Box */}
      <div 
        className={cn(
          "relative w-full overflow-hidden shadow-md transition-transform duration-300 group-hover:scale-[var(--hover-scale)]",
          isLight ? 'bg-gray-100 border border-gray-200' : 'bg-neutral-900 border border-neutral-800/80',
          classNames.thumbnail
        )}
        style={{ 
          aspectRatio,
          borderRadius: `${borderRadius}px`,
        }}
      >
        {/* Main Poster Image */}
        <img 
          src={posterUrl} 
          alt={title}
          className={cn(
            "w-full h-full object-cover transition-opacity duration-300",
            (isHovered && (currentSpriteStyle || activeFrameUrl || isVideoPlaying)) ? "opacity-0" : "opacity-100",
            classNames.poster
          )}
        />

        {/* Fallback Keyframe Sequence Preview Image */}
        {isHovered && activeFrameUrl && !currentSpriteStyle && (
          <img 
            src={activeFrameUrl}
            alt="Preview Frame"
            className={cn(
              "absolute inset-0 w-full h-full object-cover transition-all duration-150 animate-in fade-in",
              classNames.spritePreview
            )}
          />
        )}

        {/* Sprite Flipbook Canvas Preview */}
        {isHovered && currentSpriteStyle && (
          <div 
            className={cn(
              "absolute inset-0 bg-no-repeat transition-all duration-75",
              classNames.spritePreview
            )}
            style={currentSpriteStyle}
          />
        )}

        {/* Video Preview Loop */}
        {previewVideoUrl && (
          <video
            ref={videoRef}
            src={previewVideoUrl}
            muted={isMuted}
            loop
            playsInline
            className={cn(
              "absolute inset-0 w-full h-full object-cover transition-opacity duration-300",
              (isHovered && isVideoPlaying) ? "opacity-100" : "opacity-0 pointer-events-none",
              classNames.videoPreview
            )}
          />
        )}

        {/* Hover Audio Mute Toggle Button if video preview */}
        {isHovered && previewVideoUrl && isVideoPlaying && (
          <button 
            onClick={(e) => {
              e.stopPropagation();
              if (videoRef.current) {
                videoRef.current.muted = !isMuted;
                setIsMuted(!isMuted);
              }
            }}
            className={cn(
              "absolute top-2.5 right-2.5 p-1.5 rounded-full bg-black/70 text-white backdrop-blur border border-white/20 hover:scale-110 transition-transform z-20",
              classNames.muteButton
            )}
          >
            {isMuted ? <VolumeX className="h-3.5 w-3.5" /> : <Volume2 className="h-3.5 w-3.5" />}
          </button>
        )}

        {/* Top Badges (e.g. 4K, HD) */}
        {showBadge && badge && (
          <div className={cn(
            "absolute top-2.5 left-2.5 px-2 py-0.5 rounded text-[9px] font-mono font-bold uppercase tracking-wider bg-black/70 text-white backdrop-blur border border-white/10",
            classNames.badge
          )}>
            {badge}
          </div>
        )}

        {/* Bottom Duration Badge */}
        {showDuration && (
          <div className={cn(
            "absolute bottom-2.5 right-2.5 px-2 py-0.5 rounded text-[10px] font-mono font-bold bg-black/80 text-white backdrop-blur border border-white/10 flex items-center gap-1",
            classNames.duration
          )}>
            <Clock className="h-2.5 w-2.5 text-neutral-400" />
            {durationText}
          </div>
        )}

        {/* Hover Progress Line */}
        {showProgressBar && isHovered && (vttCues.length > 0 || isVideoPlayingMode) && (
          <div className={cn("absolute bottom-0 inset-x-0 h-1 bg-neutral-800/80", classNames.progressBar)}>
            <div 
              className={cn(
                "h-full transition-all duration-150", 
                isVideoPlayingMode ? "bg-red-500" : "bg-white",
                classNames.progressFill
              )} 
              style={{ 
                width: `${isVideoPlayingMode 
                  ? (previewDuration > 0 ? (previewTime / previewDuration) * 100 : 0) 
                  : hoverProgressPct}%` 
              }}
            />
          </div>
        )}
      </div>

      {/* Metadata Section */}
      <div className={cn("flex gap-3 px-0.5", classNames.metadata)}>
        {/* Channel Avatar */}
        {showAvatar && (channelAvatar ? (
          <img 
            src={channelAvatar} 
            alt={channelName} 
            className={cn(
              "h-9 w-9 rounded-full object-cover shrink-0",
              isLight ? 'border border-gray-200' : 'border border-neutral-800',
              classNames.avatar
            )}
          />
        ) : (
          <div className={cn(
            "h-9 w-9 rounded-full shrink-0 flex items-center justify-center text-xs font-mono font-bold uppercase",
            isLight ? 'bg-gray-100 border border-gray-200 text-gray-800' : 'bg-neutral-900 border border-neutral-800 text-white',
            classNames.avatar
          )}>
            {channelName.slice(0, 2)}
          </div>
        ))}

        {/* Title & Info */}
        <div className="flex flex-col gap-1 flex-1 min-w-0">
          <h3 className={cn(
            "text-xs sm:text-sm font-semibold leading-snug transition-colors",
            titleLineClamp,
            isLight ? 'text-gray-900 group-hover:text-black' : 'text-neutral-100 group-hover:text-white',
            classNames.title
          )}>
            {title}
          </h3>

          <div className={cn(
            "flex items-center gap-1 text-[11px] font-mono",
            isLight ? 'text-gray-500' : 'text-neutral-400',
            classNames.channelName
          )}>
            <span className={cn("truncate transition-colors", isLight ? 'hover:text-gray-700' : 'hover:text-neutral-200')}>{channelName}</span>
            {showVerified && isVerified && <CheckCircle2 className={cn("h-3 w-3 shrink-0", isLight ? 'text-gray-400' : 'text-neutral-400')} />}
          </div>

          <div className={cn(
            "text-[10px] font-mono",
            isLight ? 'text-gray-400' : 'text-neutral-500',
            classNames.viewsRow
          )}>
            <span>{views}</span>
            <span className="mx-1">•</span>
            <span>{uploadedAt}</span>
          </div>
        </div>

        {/* Action Menu Button */}
        {onMenuClick && (
          <button 
            onClick={(e) => { e.stopPropagation(); onMenuClick(e); }}
            className={cn(
              "p-1 opacity-0 group-hover:opacity-100 transition-all rounded h-fit",
              isLight ? 'text-gray-400 hover:text-gray-800 hover:bg-gray-100' : 'text-neutral-500 hover:text-white hover:bg-neutral-900',
              classNames.menuButton
            )}
          >
            <MoreVertical className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
};

// Duration parsing and formatting helper functions
const parseDurationToSeconds = (durStr: string): number => {
  if (!durStr) return 0;
  const parts = durStr.split(':').map(Number);
  if (parts.some(isNaN)) return 0;
  if (parts.length === 2) {
    return parts[0] * 60 + parts[1];
  }
  if (parts.length === 3) {
    return parts[0] * 3600 + parts[1] * 60 + parts[2];
  }
  return 0;
};

const formatSeconds = (totalSec: number): string => {
  if (!isFinite(totalSec) || isNaN(totalSec)) return '0:00';
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = Math.floor(totalSec % 60);
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
};
