package worker

import (
	"bytes"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ──────────────────────────────────────────────────────────
// Test Helpers: build synthetic MPEG-TS packets in memory
// ──────────────────────────────────────────────────────────

// buildTSPacket constructs a single 188-byte MPEG-TS packet with:
//   - sync byte 0x47
//   - PUSI (payload unit start indicator) set if hasPES is true
//   - adaptation_field_control = payload only (0x01)
//   - PES header with video stream ID and PTS if hasPES is true
func buildTSPacket(hasPES bool, pts int64) [188]byte {
	var pkt [188]byte
	pkt[0] = 0x47 // sync byte

	if !hasPES {
		// Simple null/filler packet — no PUSI, payload-only
		pkt[3] = 0x10 // adaptation_field_control = 01 (payload only)
		return pkt
	}

	// Set PUSI bit (bit 6 of byte 1)
	pkt[1] = 0x40
	// adaptation_field_control = 01 (payload only)
	pkt[3] = 0x10

	// PES start code: 0x00 0x00 0x01
	pkt[4] = 0x00
	pkt[5] = 0x00
	pkt[6] = 0x01
	// Stream ID: 0xE0 (video)
	pkt[7] = 0xE0
	// PES packet length (0 = unbounded, common for video)
	pkt[8] = 0x00
	pkt[9] = 0x00
	// PES header flags byte 1 (optional fields follow)
	pkt[10] = 0x80
	// PTS/DTS flags = 10 (PTS only), no other flags
	pkt[11] = 0x80
	// PES header data length = 5 bytes (for PTS)
	pkt[12] = 0x05

	// Encode 33-bit PTS into 5 bytes at offset 13–17
	encodePTS(pkt[13:18], pts)

	return pkt
}

// buildAudioTSPacket constructs a packet with audio stream ID (0xC0)
func buildAudioTSPacket(pts int64) [188]byte {
	pkt := buildTSPacket(true, pts)
	pkt[7] = 0xC0 // audio stream ID
	return pkt
}

// buildTSPacketWithAdaptation constructs a packet with adaptation field + payload
func buildTSPacketWithAdaptation(pts int64, adaptLen int) [188]byte {
	var pkt [188]byte
	pkt[0] = 0x47 // sync byte
	pkt[1] = 0x40 // PUSI set
	// adaptation_field_control = 11 (adaptation + payload)
	pkt[3] = 0x30

	// Adaptation field
	pkt[4] = byte(adaptLen) // adaptation field length
	// Fill adaptation bytes with zeros (flags + stuffing)

	base := 5 + adaptLen // payload starts after header(4) + length(1) + adaptation

	// PES start code
	pkt[base] = 0x00
	pkt[base+1] = 0x00
	pkt[base+2] = 0x01
	pkt[base+3] = 0xE0 // video
	pkt[base+4] = 0x00
	pkt[base+5] = 0x00
	pkt[base+6] = 0x80
	pkt[base+7] = 0x80 // PTS only
	pkt[base+8] = 0x05

	encodePTS(pkt[base+9:base+14], pts)

	return pkt
}

// encodePTS encodes a 33-bit PTS into 5 bytes using standard MPEG-TS layout
func encodePTS(b []byte, pts int64) {
	// byte 0: '0010' [PTS32..30] '1'
	b[0] = 0x21 | byte((pts>>29)&0x0E)
	// byte 1: [PTS29..22]
	b[1] = byte(pts >> 22)
	// byte 2: [PTS21..15] '1'
	b[2] = 0x01 | byte((pts>>14)&0xFE)
	// byte 3: [PTS14..7]
	b[3] = byte(pts >> 7)
	// byte 4: [PTS6..0] '1'
	b[4] = 0x01 | byte((pts&0x7F)<<1)
}

// writeTSFile writes a sequence of 188-byte packets to a temp file and returns the path
func writeTSFile(t *testing.T, packets ...[188]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.ts")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkt := range packets {
		if _, err := f.Write(pkt[:]); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	f.Close()
	return path
}

// ──────────────────────────────────────────────────────────
// extractPTS round-trip test
// ──────────────────────────────────────────────────────────

func TestExtractPTS_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		pts  int64
	}{
		{"zero", 0},
		{"one_second", 90000},
		{"five_seconds", 450000},
		{"large_offset", 22500000},        // 250 seconds (segment #50 with -copyts)
		{"near_wrap", (1 << 33) - 90000},  // ~26.5 hours minus 1 second
		{"max_33bit", (1 << 33) - 1},      // maximum 33-bit value
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf [5]byte
			encodePTS(buf[:], tt.pts)
			got := extractPTS(buf[:])
			if got != tt.pts {
				t.Errorf("extractPTS round-trip failed: encoded %d, decoded %d", tt.pts, got)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────
// probeDurationGo tests
// ──────────────────────────────────────────────────────────

func buildTSStream(startPTS, targetDurationSec float64, fps float64) [][188]byte {
	var packets [][188]byte
	frameCount := int(math.Round(targetDurationSec * fps))
	if frameCount < 2 {
		frameCount = 2
	}
	totalTicks := targetDurationSec * 90000.0
	stepTicks := totalTicks / float64(frameCount)
	ptsMax := int64(1 << 33)

	startTicks := int64(math.Round(startPTS * 90000.0))
	for i := 0; i < frameCount; i++ {
		maskedPTS := (startTicks + int64(math.Round(float64(i)*stepTicks))) % ptsMax
		packets = append(packets, buildTSPacket(true, maskedPTS))
	}
	return packets
}

func buildTSStreamWithAdaptation(startPTS, targetDurationSec float64, fps float64, adaptLen int) [][188]byte {
	var packets [][188]byte
	frameCount := int(math.Round(targetDurationSec * fps))
	if frameCount < 2 {
		frameCount = 2
	}
	totalTicks := targetDurationSec * 90000.0
	stepTicks := totalTicks / float64(frameCount)
	ptsMax := int64(1 << 33)

	startTicks := int64(math.Round(startPTS * 90000.0))
	for i := 0; i < frameCount; i++ {
		maskedPTS := (startTicks + int64(math.Round(float64(i)*stepTicks))) % ptsMax
		packets = append(packets, buildTSPacketWithAdaptation(maskedPTS, adaptLen))
	}
	return packets
}

func TestProbeDurationGo_BasicDuration(t *testing.T) {
	// 5.005s video at 30fps
	stream := buildTSStream(0, 5.005, 30)
	filler := buildTSPacket(false, 0)
	packets := append([][188]byte{stream[0], filler, filler}, stream[1:]...)

	path := writeTSFile(t, packets...)
	got := probeDurationGo(path)
	want := "5.005000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q", got, want)
	}
}

func TestProbeDurationGo_CopyTsNonZeroStart(t *testing.T) {
	// Simulates -copyts: segment #50 starts at 250s, 5.0s duration at 30fps
	packets := buildTSStream(250.0, 5.0, 30)

	path := writeTSFile(t, packets...)
	got := probeDurationGo(path)
	want := "5.000000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (copyts non-zero start)", got, want)
	}
}

func TestProbeDurationGo_ShortLastSegment(t *testing.T) {
	// Last segment of a video: 3.433333 seconds at 30fps (103 frames)
	packets := buildTSStream(0, 3.433333, 30)

	path := writeTSFile(t, packets...)
	got := probeDurationGo(path)
	want := "3.433333"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (short last segment)", got, want)
	}
}

func TestProbeDurationGo_PTSWrapAround(t *testing.T) {
	// PTS wraps at 2^33. Start 1s before wrap (~95343s), 5.0s duration at 30fps
	startSec := float64((1<<33)-90000) / 90000.0
	packets := buildTSStream(startSec, 5.0, 30)

	path := writeTSFile(t, packets...)
	got := probeDurationGo(path)
	want := "5.000000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (PTS wrap-around)", got, want)
	}
}

func TestProbeDurationGo_IgnoresAudioPTS(t *testing.T) {
	// Audio packet with PTS=0, video packets for 5.0s at 30fps starting at 1.0s
	audioPkt := buildAudioTSPacket(0) // should be ignored
	videoStream := buildTSStream(1.0, 5.0, 30)
	packets := append([][188]byte{audioPkt}, videoStream...)

	path := writeTSFile(t, packets...)
	got := probeDurationGo(path)
	want := "5.000000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (should ignore audio PTS)", got, want)
	}
}

func TestProbeDurationGo_WithAdaptationField(t *testing.T) {
	// Packets with adaptation field (e.g., PCR) before payload
	packets := buildTSStreamWithAdaptation(0, 5.0, 30, 10)

	path := writeTSFile(t, packets...)
	got := probeDurationGo(path)
	want := "5.000000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (adaptation field)", got, want)
	}
}

func TestProbeDurationGo_OutputFormat(t *testing.T) {
	// Verify exactly 6 decimal places (matching ffprobe format)
	packets := buildTSStream(0, 5.0, 30)

	path := writeTSFile(t, packets...)
	got := probeDurationGo(path)

	if got != "5.000000" {
		t.Errorf("probeDurationGo() = %q, want exactly \"5.000000\" (6 decimal places)", got)
	}
}

// ──────────────────────────────────────────────────────────
// Error / edge-case tests — all must return "0"
// ──────────────────────────────────────────────────────────

func TestProbeDurationGo_NonExistentFile(t *testing.T) {
	got := probeDurationGo("/nonexistent/path/test.ts")
	if got != "0" {
		t.Errorf("probeDurationGo(nonexistent) = %q, want \"0\"", got)
	}
}

func TestProbeDurationGo_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.ts")
	os.WriteFile(path, []byte{}, 0644)
	got := probeDurationGo(path)
	if got != "0" {
		t.Errorf("probeDurationGo(empty) = %q, want \"0\"", got)
	}
}

func TestProbeDurationGo_WrongFileSize(t *testing.T) {
	// File size not a multiple of 188 — invalid MPEG-TS
	path := filepath.Join(t.TempDir(), "wrong_size.ts")
	os.WriteFile(path, make([]byte, 200), 0644) // 200 != N*188
	got := probeDurationGo(path)
	if got != "0" {
		t.Errorf("probeDurationGo(wrong size) = %q, want \"0\"", got)
	}
}

func TestProbeDurationGo_CorruptedSyncByte(t *testing.T) {
	// Valid first packet, corrupted second packet (bad sync byte)
	firstPkt := buildTSPacket(true, 0)
	var badPkt [188]byte
	badPkt[0] = 0xFF // wrong sync byte

	path := writeTSFile(t, firstPkt, badPkt)
	got := probeDurationGo(path)
	if got != "0" {
		t.Errorf("probeDurationGo(corrupted) = %q, want \"0\"", got)
	}
}

func TestProbeDurationGo_NoPTSPackets(t *testing.T) {
	// File with valid TS packets but no PES/PTS — all filler
	filler1 := buildTSPacket(false, 0)
	filler2 := buildTSPacket(false, 0)

	path := writeTSFile(t, filler1, filler2)
	got := probeDurationGo(path)
	if got != "0" {
		t.Errorf("probeDurationGo(no PTS) = %q, want \"0\"", got)
	}
}

func TestProbeDurationGo_SinglePTSPacket(t *testing.T) {
	// Only one video PTS packet — firstPTS == lastPTS → duration = 0
	pkt := buildTSPacket(true, 90000)

	path := writeTSFile(t, pkt)
	got := probeDurationGo(path)
	if got != "0.000000" {
		t.Errorf("probeDurationGo(single PTS) = %q, want \"0.000000\"", got)
	}
}

func TestProbeDurationGo_RealFFmpegFile(t *testing.T) {
	// Skip if ffmpeg or ffprobe are not installed
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}

	dir := t.TempDir()
	tsPath := filepath.Join(dir, "test.ts")

	// Generate a 5-second 30fps H.264 video TS file using ffmpeg
	cmd := exec.Command(ffmpegPath, "-y", "-f", "lavfi", "-i", "testsrc=duration=5:size=640x360:rate=30", "-c:v", "libx264", tsPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to generate test video: %v", err)
	}

	// Get ffprobe duration
	probeCmd := exec.Command(ffprobePath, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", tsPath)
	out, err := probeCmd.Output()
	if err != nil {
		t.Fatalf("failed to run ffprobe: %v", err)
	}
	wantDuration := string(bytes.TrimSpace(out))

	gotDuration := probeDurationGo(tsPath)

	t.Logf("ffprobe duration: %s, probeDurationGo: %s", wantDuration, gotDuration)
}

