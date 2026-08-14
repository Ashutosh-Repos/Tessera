package coordinator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/distributed-transcoder/internal/coordinator"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/test/mocks"
)

// ─────────────────────────────────────────────────────────────
// 1. Consistent HashRing Consensus & Partition Uniformity
// ─────────────────────────────────────────────────────────────

func Test_Coordinator_HashRing_DistributionUniformity_StdDevUnderBounds(t *testing.T) {
	ring := coordinator.NewHashRing()
	nodes := []string{"node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7", "node-8", "node-9", "node-10"}
	ring.Rebuild(nodes)

	totalPartitions := 1024
	numNodes := len(nodes)
	counts := make(map[string]int)

	for p := 0; p < totalPartitions; p++ {
		owner := ring.OwnerOf(p)
		if owner == "" {
			t.Fatalf("partition %d has no owner on active ring", p)
		}
		counts[owner]++
	}

	expectedMean := float64(totalPartitions) / float64(numNodes) // ~102.4 partitions/node
	for _, node := range nodes {
		if counts[node] == 0 {
			t.Errorf("node %s was assigned 0 partitions (starvation)", node)
		}
	}

	// Verify reasonable statistical bounds
	var sumSquares float64
	for _, node := range nodes {
		delta := float64(counts[node]) - expectedMean
		sumSquares += delta * delta
	}
	stdDev := math.Sqrt(sumSquares / float64(numNodes))
	if stdDev > 75.0 {
		t.Errorf("hash ring partition distribution standard deviation too high: %.2f (mean: %.2f)", stdDev, expectedMean)
	}
}

func Test_Coordinator_HashRing_EmptyRing_HandlesGracefullyWithoutPanic(t *testing.T) {
	ring := coordinator.NewHashRing()
	ring.Rebuild([]string{}) // No active nodes

	owner := ring.OwnerOf(123)
	if owner != "" {
		t.Errorf("expected empty string owner on empty ring, got %q", owner)
	}

	owned := ring.OwnedPartitions("node-1", 1024)
	if len(owned) != 0 {
		t.Errorf("expected 0 owned partitions on empty ring, got %d", len(owned))
	}

	members := ring.Members()
	if len(members) != 0 {
		t.Errorf("expected 0 members on empty ring, got %d", len(members))
	}
}

func Test_Coordinator_HashRing_ConcurrentRebuildAndLookup_ZeroRace(t *testing.T) {
	ring := coordinator.NewHashRing()
	ring.Rebuild([]string{"node-1", "node-2", "node-3"})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					for p := 0; p < 100; p++ {
						_ = ring.OwnerOf(p)
						_ = ring.Members()
					}
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Writer goroutines rebuilding ring
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			iteration := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
					iteration++
					if iteration%2 == 0 {
						ring.Rebuild([]string{"node-1", "node-2", "node-3", "node-4"})
					} else {
						ring.Rebuild([]string{"node-1", "node-2"})
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
}

// ─────────────────────────────────────────────────────────────
// 2. Video Slicing Atom Probing & Resolution Subsetting
// ─────────────────────────────────────────────────────────────

func Test_Coordinator_Slicer_FaststartAtomProbing_MoovBeforeMdat(t *testing.T) {
	t.Run("MoovBeforeMdat_Faststart", func(t *testing.T) {
		prefix := []byte("\x00\x00\x00\x20ftypisom\x00\x00\x00\x00\x00\x00\x01\x00moov\x00\x00\x00\x00mdat")
		if !coordinator.IsFaststart(prefix) {
			t.Errorf("expected IsFaststart = true when moov is before mdat")
		}
	})

	t.Run("MdatBeforeMoov_NonFaststart", func(t *testing.T) {
		prefix := []byte("\x00\x00\x00\x20ftypisom\x00\x00\x00\x00\x00\x00\x01\x00mdat\x00\x00\x00\x00moov")
		if coordinator.IsFaststart(prefix) {
			t.Errorf("expected IsFaststart = false when mdat is before moov")
		}
	})
}

func Test_Coordinator_Slicer_CustomResolutionSubsetting_DispatchesOnlyRequestedVariants(t *testing.T) {
	mockStore := mocks.NewMockObjectStore()
	mockBus := mocks.NewMockMessageBus()

	pm := newTestPM(mockStore, mockBus)

	// Create 2 chunks
	tempDir, err := os.MkdirTemp("", "custom-res-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	for i := 0; i < 2; i++ {
		_ = os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("chunk_%03d.mp4", i)), []byte("video"), 0644)
	}

	// Manifest with ONLY Res1080p and Res480p (2 resolutions)
	manifest := models.JobManifest{
		JobID:       "test-subset-res",
		Resolutions: []models.Resolution{models.Res1080p, models.Res480p},
	}
	manifestData, _ := json.Marshal(manifest)
	manifestKey := fmt.Sprintf("jobs/partition_%d/job_test-subset-res/job_manifest.json", 2)
	_ = mockStore.PutObject(context.Background(), manifestKey, bytes.NewReader(manifestData), int64(len(manifestData)))

	count, err := pm.UploadSlices(context.Background(), "test-subset-res", tempDir)
	if err != nil {
		t.Fatalf("UploadSlices failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 segments uploaded, got %d", count)
	}

	// Verify that each dispatched task has the correct segment index and requested resolutions (1080p and 480p only)
	dispatchedTasks := make(map[string]bool)
	for _, shardTasks := range mockBus.Tasks {
		for _, msg := range shardTasks {
			var task models.SegmentTask
			if err := json.Unmarshal(msg.Data(), &task); err != nil {
				t.Fatalf("failed to unmarshal published task: %v", err)
			}
			if task.Resolution != models.Res1080p && task.Resolution != models.Res480p {
				t.Errorf("unexpected resolution in published task: %s", task.Resolution)
			}
			if task.SegmentIdx < 0 || task.SegmentIdx > 1 {
				t.Errorf("unexpected segment index in published task: %d", task.SegmentIdx)
			}
			key := fmt.Sprintf("%d:%s", task.SegmentIdx, task.Resolution)
			if dispatchedTasks[key] {
				t.Errorf("duplicate task dispatched for %s", key)
			}
			dispatchedTasks[key] = true
		}
	}

	expectedKeys := []string{"0:1080p", "0:480p", "1:1080p", "1:480p"}
	for _, k := range expectedKeys {
		if !dispatchedTasks[k] {
			t.Errorf("missing expected dispatched task for %s", k)
		}
	}
	if len(dispatchedTasks) != 4 {
		t.Errorf("expected exactly 4 unique dispatched tasks, got %d", len(dispatchedTasks))
	}
}

// ─────────────────────────────────────────────────────────────
// 3. Manifest Compilation & Formats Tests
// ─────────────────────────────────────────────────────────────

func Test_Coordinator_Manifest_RFC8216_HLSMediaPlaylist_TargetDurationRounding(t *testing.T) {
	mockStore := mocks.NewMockObjectStore()
	mockBus := mocks.NewMockMessageBus()
	pm := newTestPM(mockStore, mockBus)

	durations := map[string]string{
		"segment_000_1080p": "5.200000",
		"segment_001_1080p": "5.800000",
		"segment_002_1080p": "4.900000",
	}

	buf := pm.GenerateHLSMediaPlaylist(models.Res1080p, 3, durations)
	playlist := buf.String()

	// Max duration is 5.8s -> Target duration should round up to 6
	if !strings.Contains(playlist, "#EXT-X-TARGETDURATION:6\n") {
		t.Errorf("expected target duration 6 in playlist, got:\n%s", playlist)
	}
	if !strings.Contains(playlist, "#EXTINF:5.800000,\ntranscoded/segment_001_1080p.ts") {
		t.Errorf("missing segment 001 entry with 5.8s duration")
	}
	if !strings.HasSuffix(strings.TrimSpace(playlist), "#EXT-X-ENDLIST") {
		t.Errorf("playlist missing #EXT-X-ENDLIST suffix")
	}
}

func Test_Coordinator_Manifest_ISO23009_DASHManifest_ValidXML(t *testing.T) {
	mockStore := mocks.NewMockObjectStore()
	mockBus := mocks.NewMockMessageBus()
	pm := newTestPM(mockStore, mockBus)

	resolutions := []models.Resolution{models.Res1080p, models.Res720p, models.Res480p}
	durations := map[string]string{
		"segment_000_1080p": "5.000000",
	}

	buf := pm.GenerateDASHManifest(resolutions, 2, durations)
	dashXML := buf.String()

	if !strings.HasPrefix(dashXML, "<?xml version=\"1.0\" encoding=\"utf-8\"?>") {
		t.Errorf("missing XML prolog")
	}
	if !strings.Contains(dashXML, "urn:mpeg:dash:schema:mpd:2011") {
		t.Errorf("missing DASH schema namespace")
	}
	if !strings.Contains(dashXML, "mediaPresentationDuration=\"PT10.000S\"") {
		t.Errorf("total duration mismatch in MPD, got:\n%s", dashXML)
	}
	if !strings.Contains(dashXML, "width=\"1920\" height=\"1080\"") ||
		!strings.Contains(dashXML, "width=\"1280\" height=\"720\"") ||
		!strings.Contains(dashXML, "width=\"854\" height=\"480\"") {
		t.Errorf("missing one or more resolution representations in DASH manifest")
	}
}
