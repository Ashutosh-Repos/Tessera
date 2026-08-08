package worker

import (
	"fmt"
	"os"
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

func TestProbeDurationGo_BasicDuration(t *testing.T) {
	// Two video PES packets: PTS=0 and PTS=450450 (5.005s at 90kHz)
	firstPkt := buildTSPacket(true, 0)
	filler := buildTSPacket(false, 0) // non-PES filler
	lastPkt := buildTSPacket(true, 450450)

	path := writeTSFile(t, firstPkt, filler, filler, lastPkt)
	got := probeDurationGo(path)
	want := fmt.Sprintf("%.6f", 450450.0/90000.0) // "5.005000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q", got, want)
	}
}

func TestProbeDurationGo_CopyTsNonZeroStart(t *testing.T) {
	// Simulates -copyts: segment #50 starts at PTS=22500000 (250s)
	// Duration should be 5.0s, NOT 255.0s
	startPTS := int64(22500000) // 250 seconds
	endPTS := int64(22950000)   // 255 seconds

	firstPkt := buildTSPacket(true, startPTS)
	lastPkt := buildTSPacket(true, endPTS)

	path := writeTSFile(t, firstPkt, lastPkt)
	got := probeDurationGo(path)
	want := fmt.Sprintf("%.6f", float64(endPTS-startPTS)/90000.0) // "5.000000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (copyts non-zero start)", got, want)
	}
}

func TestProbeDurationGo_ShortLastSegment(t *testing.T) {
	// Last segment of a video: only 3.421 seconds
	startPTS := int64(0)
	endPTS := int64(307890) // 3.421 * 90000

	firstPkt := buildTSPacket(true, startPTS)
	lastPkt := buildTSPacket(true, endPTS)

	path := writeTSFile(t, firstPkt, lastPkt)
	got := probeDurationGo(path)
	want := fmt.Sprintf("%.6f", float64(endPTS)/90000.0) // "3.421000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (short last segment)", got, want)
	}
}

func TestProbeDurationGo_PTSWrapAround(t *testing.T) {
	// PTS wraps at 2^33. Start near max, end after wrap.
	startPTS := int64((1 << 33) - 90000) // 1 second before wrap
	endPTS := int64(360000)              // 4 seconds after wrap

	firstPkt := buildTSPacket(true, startPTS)
	lastPkt := buildTSPacket(true, endPTS)

	path := writeTSFile(t, firstPkt, lastPkt)
	got := probeDurationGo(path)

	// Expected: (endPTS - startPTS + 2^33) / 90000 = 5.0 seconds
	expectedDiff := endPTS - startPTS + (1 << 33)
	want := fmt.Sprintf("%.6f", float64(expectedDiff)/90000.0)

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (PTS wrap-around)", got, want)
	}
}

func TestProbeDurationGo_IgnoresAudioPTS(t *testing.T) {
	// Audio packet with PTS=0, video packets with PTS=90000..540000
	// Duration should be based on video only: (540000-90000)/90000 = 5.0s
	audioPkt := buildAudioTSPacket(0)              // should be ignored
	videoStart := buildTSPacket(true, 90000)        // 1.0s
	videoEnd := buildTSPacket(true, 540000)         // 6.0s

	path := writeTSFile(t, audioPkt, videoStart, videoEnd)
	got := probeDurationGo(path)
	want := fmt.Sprintf("%.6f", float64(540000-90000)/90000.0) // "5.000000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (should ignore audio PTS)", got, want)
	}
}

func TestProbeDurationGo_WithAdaptationField(t *testing.T) {
	// Packet with adaptation field (e.g., PCR) before payload
	firstPkt := buildTSPacketWithAdaptation(0, 10) // 10-byte adaptation field
	lastPkt := buildTSPacketWithAdaptation(450000, 10)

	path := writeTSFile(t, firstPkt, lastPkt)
	got := probeDurationGo(path)
	want := fmt.Sprintf("%.6f", 450000.0/90000.0) // "5.000000"

	if got != want {
		t.Errorf("probeDurationGo() = %q, want %q (adaptation field)", got, want)
	}
}

func TestProbeDurationGo_OutputFormat(t *testing.T) {
	// Verify exactly 6 decimal places (matching ffprobe format)
	firstPkt := buildTSPacket(true, 0)
	lastPkt := buildTSPacket(true, 450000) // exactly 5.0 seconds

	path := writeTSFile(t, firstPkt, lastPkt)
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
