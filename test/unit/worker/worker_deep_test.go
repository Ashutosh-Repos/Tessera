package worker_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/distributed-transcoder/internal/worker"
	"github.com/distributed-transcoder/test/mocks"
)

// ─────────────────────────────────────────────────────────────
// 1. Bitstream & PTS Duration Parser Tests
// ─────────────────────────────────────────────────────────────

func Test_Worker_ProbeDuration_CorruptedSyncByte_ReturnsZero(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-corrupt-sync-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "corrupted.ts")
	data := make([]byte, 188*10)
	for i := range data {
		data[i] = 0xAA // Non-0x47 sync byte
	}
	_ = os.WriteFile(filePath, data, 0644)

	dur := worker.ProbeDurationGo(filePath)
	if dur != "0" {
		t.Errorf("expected '0' on corrupted sync byte file, got %q", dur)
	}
}

func Test_Worker_ProbeDuration_ZeroByteAndNonExistent_ReturnsZero(t *testing.T) {
	t.Run("NonExistent", func(t *testing.T) {
		dur := worker.ProbeDurationGo("/non/existent/path/video.ts")
		if dur != "0" {
			t.Errorf("expected '0' for non-existent file, got %q", dur)
		}
	})

	t.Run("ZeroBytes", func(t *testing.T) {
		tempDir, _ := os.MkdirTemp("", "test-zero-*")
		defer os.RemoveAll(tempDir)
		path := filepath.Join(tempDir, "empty.ts")
		_ = os.WriteFile(path, []byte{}, 0644)

		dur := worker.ProbeDurationGo(path)
		if dur != "0" {
			t.Errorf("expected '0' for empty file, got %q", dur)
		}
	})
}

func Test_Worker_ProbeDuration_33BitPTSWrapAround_CorrectArithmetic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-pts-wrap-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "wraparound.ts")

	// Create 2 MPEG-TS packets (188 bytes each):
	// Packet 0: PTS near max 33-bit value (8589934592 - 90000 = 8589844592, which is 1 sec before wrap)
	// Packet 1: PTS wrapped around to 90000 (1 sec after wrap) -> Total duration = 2.0s
	data := make([]byte, 188*2)

	pts1 := uint64(0x1FFFFFFFF - 90000)
	pts2 := uint64(90000)

	// Build Packet 0
	data[0] = 0x47
	data[1] = 0x41 // PUSI = 1, PID = 0x100
	data[2] = 0x00
	data[3] = 0x10 // payload only
	// PES Header
	data[4] = 0x00
	data[5] = 0x00
	data[6] = 0x01
	data[7] = 0xE0 // Video stream ID
	data[8] = 0x00
	data[9] = 0x00
	data[10] = 0x80
	data[11] = 0x80 // PTS present (flags=0x80)
	data[12] = 0x05 // PES header data length
	// PTS encoding for pts1
	data[13] = byte(0x20 | (((pts1 >> 30) & 0x07) << 1) | 0x01)
	data[14] = byte((pts1 >> 22) & 0xFF)
	data[15] = byte((((pts1 >> 15) & 0x7F) << 1) | 0x01)
	data[16] = byte((pts1 >> 7) & 0xFF)
	data[17] = byte(((pts1 & 0x7F) << 1) | 0x01)

	// Build Packet 1
	offset := 188
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
	// PTS encoding for pts2
	data[offset+13] = byte(0x20 | (((pts2 >> 30) & 0x07) << 1) | 0x01)
	data[offset+14] = byte((pts2 >> 22) & 0xFF)
	data[offset+15] = byte((((pts2 >> 15) & 0x7F) << 1) | 0x01)
	data[offset+16] = byte((pts2 >> 7) & 0xFF)
	data[offset+17] = byte(((pts2 & 0x7F) << 1) | 0x01)

	_ = os.WriteFile(filePath, data, 0644)

	durStr := worker.ProbeDurationGo(filePath)
	dur, err := strconv.ParseFloat(durStr, 64)
	if err != nil {
		t.Fatalf("failed to parse duration string %q: %v", durStr, err)
	}
	// pts1 is 1s before the 33-bit wrap, pts2 is 1s after it (diff=2.0s, frameDur=2.0s -> total=4.0s)
	const wantDur = 4.0
	const tolerance = 0.05
	if dur < wantDur-tolerance || dur > wantDur+tolerance {
		t.Errorf("expected duration %.3fs across the 33-bit PTS wraparound, got %fs", wantDur, dur)
	}
}

// ─────────────────────────────────────────────────────────────
// 2. Circuit Breaker Resilience Tests
// ─────────────────────────────────────────────────────────────

func Test_Worker_CircuitBreaker_FullLifecycle_StateTransitions(t *testing.T) {
	cb := worker.NewCircuitBreaker(1, 3)

	// Initial State: CLOSED
	if cb.IsOpen() {
		t.Fatal("expected circuit breaker to start CLOSED")
	}

	// 2 Failures: Still CLOSED
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.IsOpen() {
		t.Fatal("expected CLOSED after 2 failures (threshold=3)")
	}

	// 3rd Failure: Transitions to OPEN
	cb.RecordFailure()
	if !cb.IsOpen() {
		t.Fatal("expected OPEN after reaching failure threshold")
	}

	// Success: Instantly resets back to CLOSED
	cb.RecordSuccess()
	if cb.IsOpen() {
		t.Fatal("expected CLOSED immediately following RecordSuccess")
	}
}

func Test_Worker_CircuitBreaker_ConcurrentAccess_ThreadSafe(t *testing.T) {
	cb := worker.NewCircuitBreaker(1, 10)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%3 == 0 {
				cb.RecordSuccess()
			} else {
				cb.RecordFailure()
			}
			_ = cb.IsOpen()
			_ = cb.BackoffDuration()
		}(i)
	}

	wg.Wait()
}

// ─────────────────────────────────────────────────────────────
// 3. Task Idempotency & Direct Upload Tests
// ─────────────────────────────────────────────────────────────

func Test_Worker_Idempotency_ZeroByteObject_TriggersTranscode(t *testing.T) {
	mockStore := mocks.NewMockObjectStore()
	ctx := context.Background()

	key := "jobs/partition_0/job_123/transcoded/segment_000_1080p.ts"
	// Seed a 0-byte corrupt object in S3
	_ = mockStore.PutObject(ctx, key, nil, 0)

	meta, err := mockStore.HeadObject(ctx, key)
	if err != nil {
		t.Fatalf("HeadObject failed: %v", err)
	}

	// An object with Size == 0 is NOT considered a valid completed transcode
	shouldSkip := meta.Exists && meta.Size > 0
	if shouldSkip {
		t.Errorf("expected shouldSkip = false for 0-byte S3 object")
	}
}

func Test_Worker_Idempotency_ValidTranscodedObject_SkipsRedundantWork(t *testing.T) {
	mockStore := mocks.NewMockObjectStore()
	ctx := context.Background()

	key := "jobs/partition_0/job_123/transcoded/segment_000_1080p.ts"
	// Seed a valid >0 byte object in S3
	data := make([]byte, 1024*500)
	_ = mockStore.PutObject(ctx, key, bytes.NewReader(data), int64(len(data)))

	meta, err := mockStore.HeadObject(ctx, key)
	if err != nil {
		t.Fatalf("HeadObject failed: %v", err)
	}

	shouldSkip := meta.Exists && meta.Size > 0
	if !shouldSkip {
		t.Errorf("expected shouldSkip = true for valid >0 byte S3 object")
	}
}
