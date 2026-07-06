import React, { useState, useRef, useEffect, useMemo } from 'react';
import { 
  MoreVertical, 
  CheckCircle2, 
  Clock, 
  VolumeX, 
  Volume2 
} from 'lucide-react';
import { cn } from '../lib/utils';
import { parseVTTCues, createDemoPosterDataUrl, type SpriteCue } from '../lib/vtt';

export interface VideoTileProps {
  id?: string;
  gatewayUrl?: string;          // Backend Gateway URL, e.g. http://localhost:8080
  jobId?: string;               // Transcoding Job ID, e.g. job_us-east:1234
  title: string;
  channelName: string;
  channelAvatar?: string;
  views?: string;
  uploadedAt?: string;
  duration?: string;            // e.g. "12:34"
  posterUrl?: string;
  spriteUrl?: string;           // Direct sprite sheet or VTT URL
  spriteVttUrl?: string;        // WebVTT file for thumbnail cues
  previewFrames?: string[];     // Optional array of keyframe image URLs for flipbook
  previewVideoUrl?: string;     // Optional MP4 preview or HLS URL
  badge?: string;               // e.g. "4K", "HD", "NEW"
  isVerified?: boolean;
  onClick?: () => void;
  onMenuClick?: (e: React.MouseEvent) => void;
  className?: string;
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

  const hoverTimerRef = useRef<number>(0);
  const flipbookIntervalRef = useRef<number>(0);
  const videoRef = useRef<HTMLVideoElement>(null);

  // 1. Fetch & Parse WebVTT file if provided
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

        // Preload sprite images into browser memory cache for instantaneous rendering on hover
        const uniqueUrls = Array.from(new Set(parsed.map(c => c.url)));
        uniqueUrls.forEach(url => {
          const img = new Image();
          img.src = url;
        });
      })
      .catch(err => console.warn('Failed to parse tile VTT:', err));

    return () => { isSubscribed = false; };
  }, [spriteUrl, spriteVttUrl]);

  // Unmount Timer Cleanup
  useEffect(() => {
    return () => {
      clearTimeout(hoverTimerRef.current);
      clearInterval(flipbookIntervalRef.current);
    };
  }, []);

  // 2. Handle Mouse Enter with Delay (YouTube-style 300ms hover delay)
  const handleMouseEnter = () => {
    clearTimeout(hoverTimerRef.current);
    hoverTimerRef.current = window.setTimeout(() => {
      setIsHovered(true);
    }, 300);
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

  // 3. Sprite Flipbook Cycling Animation
  const activeFrames = previewFrames || DEFAULT_PREVIEW_FRAMES;

  useEffect(() => {
    if (!isHovered) {
      clearInterval(flipbookIntervalRef.current);
      return;
    }

    if (vttCues.length > 0) {
      // Sequence through WebVTT cues at 2.5 frames per second
      flipbookIntervalRef.current = window.setInterval(() => {
        setActiveCueIndex(prev => (prev + 1) % vttCues.length);
      }, 350);
    } else {
      // Fallback keyframe image sequence flipbook
      flipbookIntervalRef.current = window.setInterval(() => {
        setActiveCueIndex(prev => (prev + 1) % activeFrames.length);
      }, 350);

      if (previewVideoUrl && videoRef.current) {
        videoRef.current.play().then(() => setIsVideoPlaying(true)).catch(() => {});
      }
    }

    return () => clearInterval(flipbookIntervalRef.current);
  }, [isHovered, vttCues, activeFrames, previewVideoUrl]);

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

    return {
      backgroundImage: `url(${cue.url})`,
      backgroundPosition: `-${cue.x}px -${cue.y}px`,
      width: `${cue.w}px`,
      height: `${cue.h}px`,
    };
  }, [isHovered, vttCues, activeCueIndex]);

  // Progress percentage during hover
  const hoverProgressPct = vttCues.length > 0 
    ? ((activeCueIndex + 1) / vttCues.length) * 100 
    : 0;

  return (
    <div 
      className={cn(
        "group relative flex flex-col gap-3 rounded-xl overflow-hidden cursor-pointer select-none transition-all duration-300 hover:scale-[1.02]",
        className
      )}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      onClick={onClick}
    >
      {/* Thumbnail / Preview Player Box */}
      <div className="relative aspect-video w-full rounded-xl overflow-hidden bg-neutral-900 border border-neutral-800/80 shadow-md">
        {/* Main Poster Image */}
        <img 
          src={posterUrl} 
          alt={title}
          className={cn(
            "w-full h-full object-cover transition-opacity duration-300",
            (isHovered && (currentSpriteStyle || activeFrameUrl || isVideoPlaying)) ? "opacity-0" : "opacity-100"
          )}
        />

        {/* Fallback Keyframe Sequence Preview Image */}
        {isHovered && activeFrameUrl && !currentSpriteStyle && (
          <img 
            src={activeFrameUrl}
            alt="Preview Frame"
            className="absolute inset-0 w-full h-full object-cover transition-all duration-150 animate-in fade-in"
          />
        )}

        {/* Sprite Flipbook Canvas Preview */}
        {isHovered && currentSpriteStyle && (
          <div className="absolute inset-0 flex items-center justify-center bg-black">
            <div 
              className="bg-no-repeat transition-all duration-75"
              style={{
                ...currentSpriteStyle,
                transform: 'scale(1.2)', // Scale to fit video aspect ratio cleanly
              }}
            />
          </div>
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
              (isHovered && isVideoPlaying) ? "opacity-100" : "opacity-0 pointer-events-none"
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
            className="absolute top-2.5 right-2.5 p-1.5 rounded-full bg-black/70 text-white backdrop-blur border border-white/20 hover:scale-110 transition-transform z-20"
          >
            {isMuted ? <VolumeX className="h-3.5 w-3.5" /> : <Volume2 className="h-3.5 w-3.5" />}
          </button>
        )}

        {/* Top Badges (e.g. 4K, HD) */}
        {badge && (
          <div className="absolute top-2.5 left-2.5 px-2 py-0.5 rounded text-[9px] font-mono font-bold uppercase tracking-wider bg-black/70 text-white backdrop-blur border border-white/10">
            {badge}
          </div>
        )}

        {/* Bottom Duration Badge */}
        <div className="absolute bottom-2.5 right-2.5 px-2 py-0.5 rounded text-[10px] font-mono font-bold bg-black/80 text-white backdrop-blur border border-white/10 flex items-center gap-1">
          <Clock className="h-2.5 w-2.5 text-neutral-400" />
          {duration}
        </div>

        {/* Hover Progress Line (YouTube-style red/white progress bar during preview) */}
        {isHovered && vttCues.length > 0 && (
          <div className="absolute bottom-0 inset-x-0 h-1 bg-neutral-800/80">
            <div 
              className="h-full bg-white transition-all duration-300" 
              style={{ width: `${hoverProgressPct}%` }}
            />
          </div>
        )}
      </div>

      {/* Metadata Section */}
      <div className="flex gap-3 px-0.5">
        {/* Channel Avatar */}
        {channelAvatar ? (
          <img 
            src={channelAvatar} 
            alt={channelName} 
            className="h-9 w-9 rounded-full object-cover border border-neutral-800 shrink-0"
          />
        ) : (
          <div className="h-9 w-9 rounded-full bg-neutral-900 border border-neutral-800 shrink-0 flex items-center justify-center text-xs font-mono font-bold text-white uppercase">
            {channelName.slice(0, 2)}
          </div>
        )}

        {/* Title & Info */}
        <div className="flex flex-col gap-1 flex-1 min-w-0">
          <h3 className="text-xs sm:text-sm font-semibold text-neutral-100 line-clamp-2 leading-snug group-hover:text-white transition-colors">
            {title}
          </h3>

          <div className="flex items-center gap-1 text-[11px] font-mono text-neutral-400">
            <span className="truncate hover:text-neutral-200 transition-colors">{channelName}</span>
            {isVerified && <CheckCircle2 className="h-3 w-3 text-neutral-400 shrink-0" />}
          </div>

          <div className="text-[10px] font-mono text-neutral-500">
            <span>{views}</span>
            <span className="mx-1">•</span>
            <span>{uploadedAt}</span>
          </div>
        </div>

        {/* Action Menu Button */}
        {onMenuClick && (
          <button 
            onClick={(e) => { e.stopPropagation(); onMenuClick(e); }}
            className="p-1 text-neutral-500 hover:text-white opacity-0 group-hover:opacity-100 transition-all rounded hover:bg-neutral-900 h-fit"
          >
            <MoreVertical className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
};
