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

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/coordinator"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/test/mocks"
)

// ─────────────────────────────────────────────────────────────
// 1. Consistent Hash Ring & Consensus Tests
// ─────────────────────────────────────────────────────────────

func Test_Coordinator_HashRing_DistributionUniformity_1024PartitionsAcross10Nodes(t *testing.T) {
	ring := coordinator.NewHashRing()
	numNodes := 10
	totalPartitions := 1024

	nodes := make([]string, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[i] = fmt.Sprintf("coord-node-%02d", i)
	}

	ring.Rebuild(nodes)

	counts := make(map[string]int)
	for p := 0; p < totalPartitions; p++ {
		owner := ring.OwnerOf(p)
		if owner == "" {
			t.Fatalf("partition %d has no owner", p)
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
		count := float64(counts[node])
		sumSquares += math.Pow(count-expectedMean, 2)
	}
	stdDev := math.Sqrt(sumSquares / float64(numNodes))
	if stdDev > 90.0 {
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
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
}

// ─────────────────────────────────────────────────────────────
// 2. Slicer Faststart & Resolution Subset Tests
// ─────────────────────────────────────────────────────────────

func Test_Coordinator_FaststartDetection_Atoms(t *testing.T) {
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

func Test_Coordinator_Slicer_CustomResolutionSubset_DispatchesOnlyRequested(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-custom-res-deep-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	_ = os.WriteFile(filepath.Join(tempDir, "chunk_000.mp4"), []byte("video data 0"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "chunk_001.mp4"), []byte("video data 1"), 0644)

	mockStore := mocks.NewMockObjectStore()
	mockBus := mocks.NewMockMessageBus()

	cfg := config.Config{}
	cfg.Coordinator.PartitionCount = 4
	cfg.Coordinator.NATSShardCount = 2
	daemon := coordinator.NewCoordinatorDaemon(cfg, "coord-1", nil, mockBus, nil, mockStore)
	pm := coordinator.NewPartitionManager(daemon, 1, 1)

	jobID := "custom-resolutions-job"
	// Request ONLY 1080p and 480p (2 resolutions instead of 3)
	manifest := models.JobManifest{
		JobID:       jobID,
		Resolutions: []models.Resolution{models.Res1080p, models.Res480p},
	}
	data, _ := json.Marshal(manifest)
	manifestKey := fmt.Sprintf("jobs/partition_1/job_%s/job_manifest.json", jobID)
	_ = mockStore.PutObject(context.Background(), manifestKey, bytes.NewReader(data), int64(len(data)))

	count, err := pm.UploadSlices(context.Background(), jobID, tempDir)
	if err != nil {
		t.Fatalf("UploadSlices failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 segments uploaded, got %d", count)
	}

	// 2 segments * 2 resolutions = 4 total tasks published
	totalPublished := 0
	for _, tasks := range mockBus.Tasks {
		totalPublished += len(tasks)
	}

	if totalPublished != 4 {
		t.Errorf("expected 4 tasks published (2 segments * 2 resolutions), got %d", totalPublished)
	}
}

// ─────────────────────────────────────────────────────────────
// 3. Manifest Compilation & Formats Tests
// ─────────────────────────────────────────────────────────────

func Test_Coordinator_Manifest_RFC8216_HLSMediaPlaylist_TargetDurationRounding(t *testing.T) {
	pm := &coordinator.PartitionManager{}

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
	pm := &coordinator.PartitionManager{}

	resolutions := []models.Resolution{models.Res1080p, models.Res720p, models.Res480p}
	durations := map[string]string{
		"segment_000_1080p": "5.000000",
		"segment_001_1080p": "5.000000",
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
