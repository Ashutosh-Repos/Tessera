package coordinator_test

import (
	"strings"
	"testing"

	"github.com/distributed-transcoder/internal/coordinator"
	"github.com/distributed-transcoder/internal/models"
)

func TestGenerateHLSMasterPlaylist_RFC8216(t *testing.T) {
	pm := &coordinator.PartitionManager{}

	resolutions := []models.Resolution{models.Res1080p, models.Res720p, models.Res480p}
	buf := pm.GenerateHLSMasterPlaylist(resolutions)
	playlist := buf.String()

	if !strings.HasPrefix(playlist, "#EXTM3U\n") {
		t.Errorf("master playlist missing #EXTM3U header")
	}
	if !strings.Contains(playlist, "#EXT-X-VERSION:3") {
		t.Errorf("master playlist missing #EXT-X-VERSION:3 tag")
	}

	if !strings.Contains(playlist, "#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\n1080p.m3u8") {
		t.Errorf("master playlist missing 1080p stream inf entry")
	}
	if !strings.Contains(playlist, "#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720\n720p.m3u8") {
		t.Errorf("master playlist missing 720p stream inf entry")
	}
	if !strings.Contains(playlist, "#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=854x480\n480p.m3u8") {
		t.Errorf("master playlist missing 480p stream inf entry")
	}
}

func TestGenerateHLSMediaPlaylist_RFC8216(t *testing.T) {
	pm := &coordinator.PartitionManager{}

	durations := map[string]string{
		"segment_000_1080p": "5.000000",
		"segment_001_1080p": "5.005000",
		"segment_002_1080p": "4.998000",
	}

	buf := pm.GenerateHLSMediaPlaylist(models.Res1080p, 3, durations)
	playlist := buf.String()

	if !strings.HasPrefix(playlist, "#EXTM3U\n") {
		t.Errorf("media playlist missing #EXTM3U header")
	}
	if !strings.Contains(playlist, "#EXT-X-TARGETDURATION:6\n") {
		t.Errorf("media playlist missing expected target duration tag, got playlist:\n%s", playlist)
	}
	if !strings.Contains(playlist, "#EXT-X-MEDIA-SEQUENCE:0") {
		t.Errorf("media playlist missing #EXT-X-MEDIA-SEQUENCE tag")
	}
	if !strings.Contains(playlist, "#EXTINF:5.005000,\ntranscoded/segment_001_1080p.ts") {
		t.Errorf("media playlist missing segment 001 entry")
	}
	if !strings.HasSuffix(strings.TrimSpace(playlist), "#EXT-X-ENDLIST") {
		t.Errorf("media playlist missing #EXT-X-ENDLIST suffix")
	}
}

func TestGenerateDASHManifest_ISO23009(t *testing.T) {
	pm := &coordinator.PartitionManager{}

	resolutions := []models.Resolution{models.Res1080p, models.Res720p}
	durations := map[string]string{
		"segment_000_1080p": "5.000000",
		"segment_001_1080p": "5.000000",
	}

	buf := pm.GenerateDASHManifest(resolutions, 2, durations)
	manifest := buf.String()

	if !strings.HasPrefix(manifest, "<?xml version=\"1.0\" encoding=\"utf-8\"?>") {
		t.Errorf("DASH manifest missing XML header")
	}
	if !strings.Contains(manifest, "<MPD xmlns") || !strings.Contains(manifest, "mediaPresentationDuration=\"PT10.000S\"") {
		t.Errorf("DASH manifest missing MPD element or total duration attribute")
	}
	if !strings.Contains(manifest, "<AdaptationSet id=\"0\" contentType=\"video\"") {
		t.Errorf("DASH manifest missing AdaptationSet element")
	}
	if !strings.Contains(manifest, "<Representation id=\"1080p\" mimeType=\"video/mp2t\" codecs=\"avc1.640028\" width=\"1920\" height=\"1080\" frameRate=\"30\" bandwidth=\"5000000\">") {
		t.Errorf("DASH manifest missing 1080p Representation element")
	}
	if !strings.Contains(manifest, "<SegmentTemplate timescale=\"1000\" duration=\"5000\" media=\"transcoded/segment_$Number%03d$_1080p.ts\" startNumber=\"0\"/>") {
		t.Errorf("DASH manifest missing 1080p SegmentTemplate element")
	}
}
