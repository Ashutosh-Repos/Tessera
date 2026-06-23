package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"time"

	"github.com/distributed-transcoder/internal/models"
)

func (pm *PartitionManager) compileManifest(ctx context.Context, jobID string) {
	status, err := pm.coord.state.GetJobStatus(ctx, jobID)
	if err != nil {
		return
	}
	total := parseInt(status["total"])
	pm.compileManifests(ctx, jobID, total)
}

func (pm *PartitionManager) compileManifests(ctx context.Context, jobID string, totalTasks int) {
	// I-3 fix: Epoch fencing — abort if we are a stale coordinator
	status, _ := pm.coord.state.GetJobStatus(ctx, jobID)
	if status["owner_epoch"] != "" {
		storedEpoch := parseInt64(status["owner_epoch"])
		if storedEpoch > pm.coord.currentEpoch {
			log.Printf("epoch fencing: stale coordinator (ours=%d, stored=%d), aborting manifest for %s",
				pm.coord.currentEpoch, storedEpoch, jobID)
			return
		}
	}

	// 1. Update phase to COMPILING
	pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
		"state":        string(models.JobPhaseCompiling),
		"last_updated": time.Now().Unix(),
		"owner_epoch":  pm.coord.currentEpoch,
	})
	pm.coord.state.PublishProgress(ctx, jobID, models.ProgressUpdate{Phase: models.JobPhaseCompiling})

	// 2. Consistency barrier: wait 1s for S3 eventual consistency
	time.Sleep(1 * time.Second)

	// 3. Load manifest to read segment count and resolutions
	manifest, err := pm.loadManifest(ctx, jobID)
	if err != nil {
		log.Printf("Job %s: failed to load manifest for compilation: %v", jobID, err)
		pm.markJobFailed(ctx, jobID, err.Error())
		return
	}

	// 4. Read durations from Redis
	durations, err := pm.coord.state.GetAllDurations(ctx, jobID)
	if err != nil {
		log.Printf("Job %s: failed to load durations from Redis: %v", jobID, err)
		durations = make(map[string]string)
	}

	// 5. Verify last segment exists in S3 (double check consistency)
	for _, res := range manifest.Resolutions {
		lastSegKey := fmt.Sprintf("jobs/partition_%d/job_%s/transcoded/segment_%03d_%s.ts",
			pm.partitionID, jobID, manifest.SegmentCount-1, res)
		meta, err := pm.coord.objStore.HeadObject(ctx, lastSegKey)
		if err != nil || !meta.Exists {
			log.Printf("Job %s: consistency check failed, last segment %s missing from S3", jobID, lastSegKey)
			pm.markJobFailed(ctx, jobID, "eventual consistency check failed: missing transcoded segments")
			return
		}
	}

	// 6. Generate and upload HLS Master + Media Playlists
	prefix := fmt.Sprintf("jobs/partition_%d/job_%s/", pm.partitionID, jobID)
	
	// Compile and upload media playlists (e.g. 1080p.m3u8)
	for _, res := range manifest.Resolutions {
		playlistBuf := pm.generateHLSMediaPlaylist(res, manifest.SegmentCount, durations)
		playlistKey := fmt.Sprintf("%s%s.m3u8", prefix, res)
		err = pm.coord.objStore.PutObject(ctx, playlistKey, bytes.NewReader(playlistBuf.Bytes()), int64(playlistBuf.Len()))
		if err != nil {
			log.Printf("Job %s: failed to upload media playlist for %s: %v", jobID, res, err)
			pm.markJobFailed(ctx, jobID, err.Error())
			return
		}
	}

	// Compile and upload master playlist
	masterBuf := pm.generateHLSMasterPlaylist(manifest.Resolutions)
	err = pm.coord.objStore.PutObject(ctx, prefix+"master.m3u8", bytes.NewReader(masterBuf.Bytes()), int64(masterBuf.Len()))
	if err != nil {
		log.Printf("Job %s: failed to upload master HLS playlist: %v", jobID, err)
		pm.markJobFailed(ctx, jobID, err.Error())
		return
	}

	// 7. Generate and upload DASH Manifest (manifest.mpd)
	dashBuf := pm.generateDASHManifest(manifest.Resolutions, manifest.SegmentCount, durations)
	err = pm.coord.objStore.PutObject(ctx, prefix+"manifest.mpd", bytes.NewReader(dashBuf.Bytes()), int64(dashBuf.Len()))
	if err != nil {
		log.Printf("Job %s: failed to upload DASH manifest: %v", jobID, err)
		pm.markJobFailed(ctx, jobID, err.Error())
		return
	}

	// 8. Write completion sentinel
	sentinelData := completedSentinel()
	err = pm.coord.objStore.PutObject(ctx, prefix+"job_completed.json", bytes.NewReader(sentinelData), int64(len(sentinelData)))
	if err != nil {
		log.Printf("Job %s: failed to upload completed sentinel: %v", jobID, err)
	}

	// 9. Update state to completed and notify client
	pm.coord.state.SetJobStatus(ctx, jobID, map[string]interface{}{
		"state":        string(models.JobPhaseCompleted),
		"last_updated": time.Now().Unix(),
	})
	pm.coord.state.PublishProgress(ctx, jobID, models.ProgressUpdate{
		Phase:   models.JobPhaseCompleted,
		HLSURL:  fmt.Sprintf("https://cdn.example.com/%smaster.m3u8", prefix),
		DASHURL: fmt.Sprintf("https://cdn.example.com/%smanifest.mpd", prefix),
	})

	// 10. Cleanup active jobs tracking in partition
	pm.coord.state.RemoveActiveJob(ctx, pm.partitionID, jobID)

	// Clean up raw files and slices from S3 to prevent disk leaks
	rawPrefix := fmt.Sprintf("jobs/partition_%d/job_%s/raw/", pm.partitionID, jobID)
	if err := pm.coord.objStore.DeletePrefix(ctx, rawPrefix); err != nil {
		log.Printf("Job %s: failed to clean up raw S3 files: %v", jobID, err)
	}

	// Expire Redis keys after 24h to prevent memory leaks (fails open)
	if err := pm.coord.state.ExpireJobKeys(ctx, jobID, 86400); err != nil {
		log.Printf("Job %s: failed to set Redis keys expiration: %v", jobID, err)
	}

	log.Printf("Job %s: successfully compiled HLS and DASH manifests", jobID)
}

func (pm *PartitionManager) generateHLSMasterPlaylist(resolutions []models.Resolution) *bytes.Buffer {
	buf := bytes.NewBufferString("#EXTM3U\n")
	buf.WriteString("#EXT-X-VERSION:3\n\n")

	for _, res := range resolutions {
		bandwidth := 1000000
		dimensions := "854x480"
		switch res {
		case models.Res1080p:
			bandwidth = 5000000
			dimensions = "1920x1080"
		case models.Res720p:
			bandwidth = 2500000
			dimensions = "1280x720"
		}
		buf.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bandwidth, dimensions))
		buf.WriteString(fmt.Sprintf("%s.m3u8\n", res))
	}
	return buf
}

func (pm *PartitionManager) generateHLSMediaPlaylist(res models.Resolution, segmentCount int, durations map[string]string) *bytes.Buffer {
	buf := bytes.NewBufferString("#EXTM3U\n")
	buf.WriteString("#EXT-X-VERSION:3\n")
	
	// Determine target duration
	maxDuration := 5.0
	for seg := 0; seg < segmentCount; seg++ {
		key := fmt.Sprintf("segment_%03d_%s", seg, res)
		if val, ok := durations[key]; ok {
			if d, err := strconv.ParseFloat(val, 64); err == nil && d > maxDuration {
				maxDuration = d
			}
		}
	}
	buf.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(maxDuration)+1))
	buf.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n\n")

	for seg := 0; seg < segmentCount; seg++ {
		key := fmt.Sprintf("segment_%03d_%s", seg, res)
		durStr := "5.000000"
		if val, ok := durations[key]; ok {
			durStr = val
		}
		buf.WriteString(fmt.Sprintf("#EXTINF:%s,\n", durStr))
		buf.WriteString(fmt.Sprintf("transcoded/segment_%03d_%s.ts\n", seg, res))
	}

	buf.WriteString("#EXT-X-ENDLIST\n")
	return buf
}

func (pm *PartitionManager) generateDASHManifest(resolutions []models.Resolution, segmentCount int, durations map[string]string) *bytes.Buffer {
	// Calculate total duration using one resolution
	totalDur := 0.0
	refRes := models.Res1080p
	if len(resolutions) > 0 {
		refRes = resolutions[0]
	}
	for seg := 0; seg < segmentCount; seg++ {
		key := fmt.Sprintf("segment_%03d_%s", seg, refRes)
		if val, ok := durations[key]; ok {
			if d, err := strconv.ParseFloat(val, 64); err == nil {
				totalDur += d
			}
		} else {
			totalDur += 5.0 // fallback
		}
	}

	buf := bytes.NewBufferString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n")
	buf.WriteString("<MPD xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\"\n")
	buf.WriteString("     xmlns=\"urn:mpeg:dash:schema:mpd:2011\"\n")
	buf.WriteString("     xsi:schemaLocation=\"urn:mpeg:dash:schema:mpd:2011 DASH-MPD.xsd\"\n")
	buf.WriteString("     profiles=\"urn:mpeg:dash:profile:isoff-live:2011\"\n")
	buf.WriteString("     type=\"static\"\n")
	buf.WriteString(fmt.Sprintf("     mediaPresentationDuration=\"PT%.3fS\"\n", totalDur))
	buf.WriteString("     minBufferTime=\"PT1.5S\">\n")
	buf.WriteString("  <Period id=\"0\" start=\"PT0.0S\">\n")
	buf.WriteString("    <AdaptationSet id=\"0\" contentType=\"video\" segmentAlignment=\"true\" bitstreamSwitching=\"true\">\n")

	for _, res := range resolutions {
		bandwidth := 1000000
		width, height := 854, 480
		switch res {
		case models.Res1080p:
			bandwidth = 5000000
			width, height = 1920, 1080
		case models.Res720p:
			bandwidth = 2500000
			width, height = 1280, 720
		}
		
		buf.WriteString(fmt.Sprintf("      <Representation id=\"%s\" mimeType=\"video/mp4\" codecs=\"avc1.640028\" width=\"%d\" height=\"%d\" frameRate=\"30\" bandwidth=\"%d\">\n",
			res, width, height, bandwidth))
		buf.WriteString(fmt.Sprintf("        <SegmentTemplate timescale=\"1000\" duration=\"5000\" media=\"transcoded/segment_$Number%%03d$_%s.ts\" startNumber=\"0\"/>\n", res))
		buf.WriteString("      </Representation>\n")
	}

	buf.WriteString("    </AdaptationSet>\n")
	buf.WriteString("  </Period>\n")
	buf.WriteString("</MPD>\n")

	return buf
}

func completedSentinel() []byte {
	return []byte("{\"status\":\"completed\"}")
}

func (pm *PartitionManager) loadManifest(ctx context.Context, jobID string) (*models.JobManifest, error) {
	key := fmt.Sprintf("jobs/partition_%d/job_%s/job_manifest.json", pm.partitionID, jobID)
	rc, err := pm.coord.objStore.GetObject(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var manifest models.JobManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}
