package coordinator

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// generateAssets runs ffmpeg to extract frames from individual slices in tempDir,
// tiles them into a sprite sheet, creates the WebVTT file, extracts large thumbnails,
// uploads all assets to S3, and stores their paths in the Redis job status.
func (pm *PartitionManager) generateAssets(ctx context.Context, jobID string, tempDir string, segmentCount int, duration float64) error {
	log.Printf("Job %s: Starting asset generation (segmentCount=%d, duration=%.2fs)", jobID, segmentCount, duration)

	if segmentCount <= 0 {
		return fmt.Errorf("invalid segment count: %d", segmentCount)
	}

	// 1. Extract sprite cell frames (160x90) from each segment chunk
	var cellPaths []string
	step := 1
	if segmentCount > 100 {
		step = segmentCount / 100
	}

	for i := 0; i < segmentCount; i += step {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", i))
		cellPath := filepath.Join(tempDir, fmt.Sprintf("cell_%03d.jpg", i))

		// Execute ffmpeg to grab the first frame of the segment
		cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", chunkPath, "-vframes", "1", "-vf",
			"scale=160:90:force_original_aspect_ratio=decrease,pad=160:90:(ow-iw)/2:(oh-ih)/2", cellPath)
		if err := cmd.Run(); err != nil {
			log.Printf("Job %s: failed to extract sprite cell for segment %d: %v", jobID, i, err)
			continue
		}
		cellPaths = append(cellPaths, cellPath)
	}

	if len(cellPaths) == 0 {
		return fmt.Errorf("failed to extract any sprite cells")
	}

	// 2. Tile cells together into a single sprite sheet
	spriteLocalPath := filepath.Join(tempDir, "sprite.jpg")
	if err := tileImages(cellPaths, spriteLocalPath); err != nil {
		return fmt.Errorf("failed to tile images: %w", err)
	}

	// 3. Generate WebVTT file
	vttLocalPath := filepath.Join(tempDir, "sprite.vtt")
	if err := generateWebVTTFile(vttLocalPath, len(cellPaths), duration); err != nil {
		return fmt.Errorf("failed to generate WebVTT file: %w", err)
	}

	// 4. Extract 3 large thumbnails (640x360) from start, middle, and end chunks
	var thumbLocalPaths []string
	indices := []int{0, len(cellPaths) / 2, len(cellPaths) - 1}
	// Deduplicate indices for very short videos
	uniqueIndices := make([]int, 0, 3)
	seen := make(map[int]bool)
	for _, idx := range indices {
		if !seen[idx] {
			seen[idx] = true
			uniqueIndices = append(uniqueIndices, idx)
		}
	}

	for i, idx := range uniqueIndices {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", idx))
		thumbPath := filepath.Join(tempDir, fmt.Sprintf("large_thumb_%d.jpg", i))

		cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", chunkPath, "-vframes", "1", "-vf",
			"scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2", thumbPath)
		if err := cmd.Run(); err != nil {
			log.Printf("Job %s: failed to extract thumbnail option %d: %v", jobID, i, err)
			continue
		}
		thumbLocalPaths = append(thumbLocalPaths, thumbPath)
	}

	// 5. Upload all assets to S3
	prefix := fmt.Sprintf("jobs/partition_%d/job_%s/", pm.partitionID, jobID)

	// Upload Sprite Image
	spriteKey := prefix + "sprite/sprite.jpg"
	if err := pm.uploadLocalFile(ctx, spriteLocalPath, spriteKey); err != nil {
		return fmt.Errorf("failed to upload sprite sheet: %w", err)
	}

	// Upload WebVTT File
	vttKey := prefix + "sprite/sprite.vtt"
	if err := pm.uploadLocalFile(ctx, vttLocalPath, vttKey); err != nil {
		return fmt.Errorf("failed to upload WebVTT: %w", err)
	}

	// Upload Thumbnail Options
	var thumbnailKeys []string
	for i, thumbPath := range thumbLocalPaths {
		thumbKey := fmt.Sprintf("%sthumbnails/thumb_%d.jpg", prefix, i)
		if err := pm.uploadLocalFile(ctx, thumbPath, thumbKey); err != nil {
			log.Printf("Job %s: failed to upload thumbnail option %d: %v", jobID, i, err)
			continue
		}
		thumbnailKeys = append(thumbnailKeys, thumbKey)
	}

	// 6. Write asset paths to Redis Job Status Hash so Gateway / Sync Service can fetch them
	statusUpdates := map[string]interface{}{
		"sprite_key":   spriteKey,
		"sprite_vtt":   vttKey,
		"duration":     duration,
		"last_updated": time.Now().Unix(),
	}

	for i, key := range thumbnailKeys {
		statusUpdates[fmt.Sprintf("thumbnail_%d", i)] = key
	}

	// Probe video details (width, height, fps) from the first chunk
	if segmentCount > 0 {
		firstChunk := filepath.Join(tempDir, "chunk_000.mp4")
		if w, h, f, err := probeChunkMetadata(ctx, firstChunk); err == nil {
			statusUpdates["width"] = w
			statusUpdates["height"] = h
			statusUpdates["fps"] = f
			log.Printf("Job %s: Probed metadata - %dx%d, %dfps", jobID, w, h, f)
		} else {
			log.Printf("Job %s: Failed to probe chunk metadata: %v", jobID, err)
		}
	}

	if err := pm.coord.state.SetJobStatus(ctx, jobID, statusUpdates); err != nil {
		log.Printf("Job %s: failed to save asset paths to Redis status: %v", jobID, err)
	}

	log.Printf("Job %s: Asset generation complete. Uploaded sprite and %d thumbnails.", jobID, len(thumbnailKeys))
	return nil
}

func probeChunkMetadata(ctx context.Context, chunkPath string) (int, int, int, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,r_frame_rate", "-of", "csv=p=0", chunkPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0, 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(out.String()), ",")
	if len(parts) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid ffprobe output: %s", out.String())
	}
	width, _ := strconv.Atoi(parts[0])
	height, _ := strconv.Atoi(parts[1])

	fpsParts := strings.Split(parts[2], "/")
	fps := 30
	if len(fpsParts) == 2 {
		num, _ := strconv.Atoi(fpsParts[0])
		den, _ := strconv.Atoi(fpsParts[1])
		if den > 0 {
			fps = num / den
		}
	} else {
		fps, _ = strconv.Atoi(parts[2])
	}

	return width, height, fps, nil
}

func getChunkDuration(ctx context.Context, chunkPath string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", chunkPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	val := strings.TrimSpace(out.String())
	dur, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, err
	}
	return dur, nil
}

func (pm *PartitionManager) uploadLocalFile(ctx context.Context, localPath string, destKey string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	return pm.coord.objStore.PutObject(ctx, destKey, f, stat.Size())
}

func tileImages(cellPaths []string, destPath string) error {
	const cellW = 160
	const cellH = 90
	const cols = 10
	total := len(cellPaths)
	if total == 0 {
		return fmt.Errorf("no cell images to tile")
	}

	rows := (total + cols - 1) / cols
	destW := cellW * cols
	destH := cellH * rows

	destImg := image.NewRGBA(image.Rect(0, 0, destW, destH))

	for i, path := range cellPaths {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return err
		}

		col := i % cols
		row := i / cols
		x := col * cellW
		y := row * cellH

		rect := image.Rect(x, y, x+cellW, y+cellH)
		draw.Draw(destImg, rect, img, image.Point{}, draw.Src)
	}

	outF, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer outF.Close()

	return jpeg.Encode(outF, destImg, &jpeg.Options{Quality: 85})
}

func generateWebVTTFile(destPath string, total int, duration float64) error {
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString("WEBVTT\n\n"); err != nil {
		return err
	}

	const cellW = 160
	const cellH = 90
	const cols = 10

	interval := 5.0
	if duration > 0 && total > 0 {
		interval = duration / float64(total)
	}

	for i := 0; i < total; i++ {
		startTime := float64(i) * interval
		endTime := float64(i+1) * interval
		if endTime > duration {
			endTime = duration
		}

		formatTime := func(t float64) string {
			h := int(t) / 3600
			m := (int(t) % 3600) / 60
			s := int(t) % 60
			ms := int((t - float64(int(t))) * 1000)
			return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
		}

		col := i % cols
		row := i / cols
		x := col * cellW
		y := row * cellH

		entry := fmt.Sprintf("%s --> %s\n", formatTime(startTime), formatTime(endTime))
		entry += fmt.Sprintf("sprite.jpg#xywh=%d,%d,%d,%d\n\n", x, y, cellW, cellH)

		if _, err := f.WriteString(entry); err != nil {
			return err
		}
	}

	return nil
}
