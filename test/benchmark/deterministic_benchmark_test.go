package benchmark_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/distributed-transcoder/internal/coordinator"
	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/internal/models"
	"github.com/distributed-transcoder/internal/worker"
)

// ─────────────────────────────────────────────────────────────
// 1. Consistent Hash Ring: Binary Search Scaling O(log V)
// ─────────────────────────────────────────────────────────────

func Benchmark_HashRing_OwnerOf_Scaling_Deterministic(b *testing.B) {
	nodeCounts := []int{10, 100, 1000} // V = 1,500; 15,000; 150,000 virtual nodes

	for _, numNodes := range nodeCounts {
		b.Run(fmt.Sprintf("Nodes_%d_VNodes_%d", numNodes, numNodes*150), func(b *testing.B) {
			ring := coordinator.NewHashRing()
			nodes := make([]string, numNodes)
			for i := 0; i < numNodes; i++ {
				nodes[i] = fmt.Sprintf("node-%04d", i)
			}
			ring.Rebuild(nodes)

			// Verify zero heap allocation per lookup
			allocs := testing.AllocsPerRun(1000, func() {
				_ = ring.OwnerOf(42)
			})
			if allocs > 0 {
				b.Errorf("OwnerOf allocated %f heap objects, want 0", allocs)
			}

			// Theoretical Max Binary Search Comparisons = ceil(log2(numNodes * 150))
			vnodes := float64(numNodes * 150)
			maxTheoreticalSteps := math.Ceil(math.Log2(vnodes))

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = ring.OwnerOf(i % 1024)
			}

			b.ReportMetric(maxTheoreticalSteps, "max_cmp_steps")
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 2. MPEG-TS PTS Byte Parser: O(1) per packet, 0 Allocs
// ─────────────────────────────────────────────────────────────

func Benchmark_ExtractPTS_Deterministic(b *testing.B) {
	rawPTSBytes := []byte{0x21, 0x00, 0x01, 0x00, 0x01} // 5-byte PES PTS layout

	// Verify deterministic zero heap allocation
	allocs := testing.AllocsPerRun(1000, func() {
		_ = worker.ExtractPTS(rawPTSBytes)
	})
	if allocs > 0 {
		b.Errorf("ExtractPTS allocated %f heap objects, want 0", allocs)
	}

	b.ResetTimer()
	b.ReportAllocs()

	var sink int64
	for i := 0; i < b.N; i++ {
		sink += worker.ExtractPTS(rawPTSBytes)
	}
	_ = sink
}

func Benchmark_ProbeDurationGo_Deterministic(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "bench-probe-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "sample_5s.ts")

	// Generate 150 MPEG-TS packets (188 bytes each) representing 5 seconds of 30fps video
	packetCount := 150
	data := make([]byte, 188*packetCount)
	for i := 0; i < packetCount; i++ {
		offset := i * 188
		data[offset+0] = 0x47
		data[offset+1] = 0x41
		data[offset+2] = 0x00
		data[offset+3] = 0x10
		data[offset+4] = 0x00
		data[offset+5] = 0x00
		data[offset+6] = 0x01
		data[offset+7] = 0xE0
		data[offset+8] = 0x00
		data[offset+9] = 0x00
		data[offset+10] = 0x80
		data[offset+11] = 0x80
		data[offset+12] = 0x05

		pts := uint64(i * 3000) // 90kHz / 30fps = 3000 ticks/frame
		data[offset+13] = byte(0x20 | (((pts >> 30) & 0x07) << 1) | 0x01)
		data[offset+14] = byte((pts >> 22) & 0xFF)
		data[offset+15] = byte((((pts >> 15) & 0x7F) << 1) | 0x01)
		data[offset+16] = byte((pts >> 7) & 0xFF)
		data[offset+17] = byte(((pts & 0x7F) << 1) | 0x01)
	}

	_ = os.WriteFile(filePath, data, 0644)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		durStr := worker.ProbeDurationGo(filePath)
		if durStr != "5.000000" {
			b.Fatalf("ProbeDurationGo() = %s, want 5.000000", durStr)
		}
	}
}

// ─────────────────────────────────────────────────────────────
// 3. Progress Multiplexer: Fanout Complexity O(S)
// ─────────────────────────────────────────────────────────────

func Benchmark_ProgressMultiplexer_FanoutScaling_Deterministic(b *testing.B) {
	subscriberCounts := []int{10, 100, 1000, 5000}

	for _, subCount := range subscriberCounts {
		b.Run(fmt.Sprintf("Subscribers_%d", subCount), func(b *testing.B) {
			channels := make([]chan models.ProgressUpdate, subCount)
			for i := 0; i < subCount; i++ {
				channels[i] = make(chan models.ProgressUpdate, 10)
				// Background drain to prevent full channels
				go func(c chan models.ProgressUpdate) {
					for range c {
					}
				}(channels[i])
			}

			update := models.ProgressUpdate{
				Phase:     models.JobPhaseTranscoding,
				Completed: 50,
				Total:     100,
				Percent:   50,
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Simulates multiplexer fanout loop across S subscribers
				for _, ch := range channels {
					select {
					case ch <- update:
					default:
					}
				}
			}

			b.StopTimer()
			for _, ch := range channels {
				close(ch)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 4. Redis Bitmap & Partition Arithmetic: O(1), 0 Allocs
// ─────────────────────────────────────────────────────────────

func Benchmark_BitIndex_Deterministic(b *testing.B) {
	task := models.SegmentTask{
		SegmentIdx: 500,
		Resolution: models.Res720p,
	}

	allocs := testing.AllocsPerRun(1000, func() {
		_ = task.BitIndex()
	})
	if allocs > 0 {
		b.Errorf("BitIndex allocated %f heap objects, want 0", allocs)
	}

	b.ResetTimer()
	b.ReportAllocs()

	var sink int
	for i := 0; i < b.N; i++ {
		sink += task.BitIndex()
	}
	_ = sink
}

func Benchmark_PartitionOf_Deterministic(b *testing.B) {
	jobID := "us-east-1:7c9e6679-7425-40de-944b-e07fc1f90ae7"

	allocs := testing.AllocsPerRun(1000, func() {
		_ = models.PartitionOf(jobID, 1024)
	})
	if allocs > 0 {
		b.Errorf("PartitionOf allocated %f heap objects, want 0", allocs)
	}

	b.ResetTimer()
	b.ReportAllocs()

	var sink int
	for i := 0; i < b.N; i++ {
		sink += models.PartitionOf(jobID, 1024)
	}
	_ = sink
}

func Benchmark_CalculateTotalParts_Deterministic(b *testing.B) {
	fileSize := int64(10 * 1024 * 1024 * 1024) // 10GB
	partSize := int64(50 * 1024 * 1024)        // 50MB

	allocs := testing.AllocsPerRun(1000, func() {
		_ = gateway.CalculateTotalParts(fileSize, partSize)
	})
	if allocs > 0 {
		b.Errorf("CalculateTotalParts allocated %f heap objects, want 0", allocs)
	}

	b.ResetTimer()
	b.ReportAllocs()

	var sink int
	for i := 0; i < b.N; i++ {
		sink += gateway.CalculateTotalParts(fileSize, partSize)
	}
	_ = sink
}
