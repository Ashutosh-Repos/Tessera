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
  const parts = ts.split(':');
  let seconds = 0;
  if (parts.length === 3) {
    seconds = parseFloat(parts[0]) * 3600 + parseFloat(parts[1]) * 60 + parseFloat(parts[2]);
  } else if (parts.length === 2) {
    seconds = parseFloat(parts[0]) * 60 + parseFloat(parts[1]);
  }
  return seconds;
}
