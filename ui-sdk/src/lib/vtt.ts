// WebVTT Parser for Sprite Thumbnail Scrubbing

export interface SpriteCue {
  startTime: number;
  endTime: number;
  url: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export function parseVTTCues(vttText: string, baseUrl?: string): SpriteCue[] {
  const cues: SpriteCue[] = [];
  const lines = vttText.split(/\r?\n/);
  
  let currentCue: Partial<SpriteCue> | null = null;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line || line.startsWith('WEBVTT') || line.startsWith('NOTE')) continue;

    // Check for timestamp arrow -->
    if (line.includes('-->')) {
      const parts = line.split('-->').map(s => s.trim());
      if (parts.length === 2) {
        const start = parseVTTTimestamp(parts[0]);
        const end = parseVTTTimestamp(parts[1]);
        currentCue = { startTime: start, endTime: end };
      }
    } else if (currentCue && currentCue.startTime !== undefined) {
      // Line contains spatial URL, e.g. sprite.jpg#xywh=0,0,160,90
      const match = line.match(/^(.*?)#xywh=(\d+),(\d+),(\d+),(\d+)$/);
      if (match) {
        let imageUrl = match[1];
        if (baseUrl && !imageUrl.startsWith('http://') && !imageUrl.startsWith('https://') && !imageUrl.startsWith('/')) {
          const basePath = baseUrl.substring(0, baseUrl.lastIndexOf('/') + 1);
          imageUrl = basePath + imageUrl;
        }

        cues.push({
          startTime: currentCue.startTime!,
          endTime: currentCue.endTime!,
          url: imageUrl,
          x: parseInt(match[2], 10),
          y: parseInt(match[3], 10),
          w: parseInt(match[4], 10),
          h: parseInt(match[5], 10),
        });
      }
      currentCue = null;
    }
  }

  return cues;
}

function parseVTTTimestamp(ts: string): number {
  const cleanTs = ts.trim().split(/\s+/)[0];
  const parts = cleanTs.split(':');
  let seconds = 0;
  if (parts.length === 3) {
    seconds = parseFloat(parts[0]) * 3600 + parseFloat(parts[1]) * 60 + parseFloat(parts[2]);
  } else if (parts.length === 2) {
    seconds = parseFloat(parts[0]) * 60 + parseFloat(parts[1]);
  }
  return seconds;
}

/**
 * Generates SpriteCue array matching Go backend algorithm (160x90 cell, 10 columns per row)
 * Matches internal/coordinator/assets.go generateWebVTTFile
 */
export function generateSpriteCuesForDuration(
  durationSec: number, 
  totalFrames: number = 20, 
  spriteImageUrl: string = 'sprite.jpg'
): SpriteCue[] {
  const cues: SpriteCue[] = [];
  const cellW = 160;
  const cellH = 90;
  const cols = 10;
  const interval = durationSec > 0 && totalFrames > 0 ? durationSec / totalFrames : 5.0;

  for (let i = 0; i < totalFrames; i++) {
    const startTime = i * interval;
    const endTime = Math.min((i + 1) * interval, durationSec);
    const col = i % cols;
    const row = Math.floor(i / cols);
    const x = col * cellW;
    const y = row * cellH;

    cues.push({
      startTime,
      endTime,
      url: spriteImageUrl,
      x,
      y,
      w: cellW,
      h: cellH,
    });
  }

  return cues;
}

/**
 * Creates a self-contained SVG Data URL sprite sheet (10 columns x 2 rows, 1600x180px)
 * Eliminates external third-party raw GitHub image dependencies.
 */
export function createDemoSpriteDataUrl(): string {
  const cellW = 160;
  const cellH = 90;
  const cols = 10;
  const rows = 2;
  const width = cellW * cols;
  const height = cellH * rows;

  let tilesSvg = '';
  for (let i = 0; i < cols * rows; i++) {
    const col = i % cols;
    const row = Math.floor(i / cols);
    const x = col * cellW;
    const y = row * cellH;
    const sec = i * 5;
    const minStr = Math.floor(sec / 60);
    const secStr = (sec % 60).toString().padStart(2, '0');
    const hue = (i * 18) % 360;

    tilesSvg += `<g transform="translate(${x}, ${y})"><rect width="${cellW}" height="${cellH}" fill="hsl(${hue}, 60%, 15%)" stroke="#333" stroke-width="1"/><circle cx="80" cy="45" r="22" fill="hsl(${hue}, 70%, 25%)" opacity="0.8"/><polygon points="74,37 90,45 74,53" fill="#ffffff" opacity="0.9"/><rect x="10" y="65" width="140" height="18" rx="4" fill="rgba(0,0,0,0.7)"/><text x="80" y="78" fill="#00ffcc" font-family="monospace" font-size="10" font-weight="bold" text-anchor="middle">${minStr}:${secStr} FRAME #${i + 1}</text></g>`;
  }

  const svgString = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"><rect width="${width}" height="${height}" fill="#0a0a0a"/>${tilesSvg}</svg>`;

  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svgString)}`;
}

/**
 * Creates a self-contained 16:9 SVG Data URL poster (800x450px)
 */
export function createDemoPosterDataUrl(title: string = 'Transcoded Video Stream', hue: number = 210): string {
  const cleanTitle = title.replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const svgString = `<svg xmlns="http://www.w3.org/2000/svg" width="800" height="450" viewBox="0 0 800 450"><defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="hsl(${hue}, 70%, 12%)" /><stop offset="100%" stop-color="hsl(${(hue + 40) % 360}, 80%, 6%)" /></linearGradient></defs><rect width="800" height="450" fill="url(#g)"/><circle cx="400" cy="225" r="70" fill="hsl(${hue}, 80%, 25%)" opacity="0.6"/><circle cx="400" cy="225" r="45" fill="hsl(${hue}, 90%, 40%)" opacity="0.9"/><polygon points="390,205 420,225 390,245" fill="#ffffff"/><rect x="40" y="360" width="720" height="50" rx="8" fill="rgba(0,0,0,0.6)" stroke="rgba(255,255,255,0.1)"/><text x="60" y="392" fill="#ffffff" font-family="sans-serif" font-size="16" font-weight="bold">${cleanTitle}</text></svg>`;

  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svgString)}`;
}

/**
 * Creates a self-contained circular SVG Data URL avatar (120x120px)
 */
export function createDemoAvatarDataUrl(initials: string = 'DM', hue: number = 210): string {
  const svgString = `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="120" viewBox="0 0 120 120"><circle cx="60" cy="60" r="60" fill="hsl(${hue}, 70%, 20%)"/><text x="60" y="70" fill="#ffffff" font-family="sans-serif" font-size="42" font-weight="bold" text-anchor="middle">${initials}</text></svg>`;

  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svgString)}`;
}
