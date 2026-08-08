package worker

import (
	"fmt"
	"io"
	"os"
)

const (
	tsPacketSize = 188     // MPEG-TS fixed packet size
	tsSyncByte   = 0x47    // Every valid TS packet starts with this byte
	ptsClockRate = 90000.0 // PTS ticks per second (90 kHz clock)
	ptsMaxValue  = 1 << 33 // PTS is a 33-bit counter; wraps at 2^33
)

// probeDurationGo reads an MPEG-TS (.ts) file and computes its duration
// by extracting the first and last video Presentation Timestamps (PTS).
//
// This is a drop-in replacement for spawning an external `ffprobe` process.
// It returns the duration as a float string with 6 decimal places (e.g. "5.005000"),
// matching the exact output format of `ffprobe -show_entries format=duration`.
//
// Returns "0" on any error (same behavior as the original ffprobe fallback).
func probeDurationGo(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return "0"
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return "0"
	}

	// Validate basic MPEG-TS structure: file size must be a multiple of 188 bytes
	if fi.Size()%tsPacketSize != 0 {
		return "0"
	}

	var firstPTS int64 = -1
	var lastPTS int64 = -1

	packet := make([]byte, tsPacketSize)

	for {
		_, err := io.ReadFull(f, packet)
		if err != nil {
			break // EOF or read error
		}

		// ── Validate sync byte ──
		if packet[0] != tsSyncByte {
			return "0" // corrupted file
		}

		// ── Check Payload Unit Start Indicator (PUSI) ──
		// PUSI is bit 6 of byte 1. When set, this packet starts a new PES packet.
		pusi := (packet[1] & 0x40) != 0
		if !pusi {
			continue // no PES header in this packet, skip
		}

		// ── Check adaptation field control (bits 4-5 of byte 3) ──
		// 01 = payload only, 10 = adaptation only, 11 = both
		adaptControl := (packet[3] & 0x30) >> 4
		payloadOffset := 4 // default: header is 4 bytes

		if adaptControl == 0x02 {
			continue // adaptation field only, no payload
		}
		if adaptControl == 0x03 {
			// Adaptation field present before payload — skip it
			if payloadOffset >= tsPacketSize {
				continue
			}
			adaptLen := int(packet[payloadOffset])
			payloadOffset += 1 + adaptLen
		}

		// ── Check for PES start code: 0x00 0x00 0x01 ──
		if payloadOffset+9 >= tsPacketSize {
			continue // not enough room for PES header
		}
		if packet[payloadOffset] != 0x00 || packet[payloadOffset+1] != 0x00 || packet[payloadOffset+2] != 0x01 {
			continue // not a PES packet start
		}

		// ── Check stream ID: video streams are 0xE0–0xEF ──
		streamID := packet[payloadOffset+3]
		if streamID < 0xE0 || streamID > 0xEF {
			continue // skip audio and other streams
		}

		// ── Check PTS/DTS flags in PES header (bits 6-7 of byte at offset+7) ──
		ptsDtsFlags := (packet[payloadOffset+7] & 0xC0) >> 6
		if ptsDtsFlags < 0x02 {
			continue // no PTS present (0x00 = none, 0x01 = forbidden)
		}

		// ── Extract 33-bit PTS from 5 bytes at offset+9 ──
		ptsStart := payloadOffset + 9
		if ptsStart+5 > tsPacketSize {
			continue // not enough bytes for PTS
		}

		pts := extractPTS(packet[ptsStart : ptsStart+5])

		if firstPTS < 0 {
			firstPTS = pts
		}
		lastPTS = pts
	}

	if firstPTS < 0 || lastPTS < 0 {
		return "0" // no video PTS found
	}

	// ── Handle 33-bit PTS wrap-around ──
	diff := lastPTS - firstPTS
	if diff < 0 {
		diff += ptsMaxValue
	}

	duration := float64(diff) / ptsClockRate
	return fmt.Sprintf("%.6f", duration)
}

// extractPTS decodes a 33-bit Presentation Timestamp from 5 bytes
// using the standard MPEG-TS PTS encoding layout:
//
//	byte 0: [0 0 1 x] [PTS32..30] [marker_bit]
//	byte 1: [PTS29..22]
//	byte 2: [PTS21..15] [marker_bit]
//	byte 3: [PTS14..7]
//	byte 4: [PTS6..0]  [marker_bit]
func extractPTS(b []byte) int64 {
	_ = b[4] // bounds check hint

	pts := int64(b[0]&0x0E) << 29 // bits 32..30
	pts |= int64(b[1]) << 22       // bits 29..22
	pts |= int64(b[2]&0xFE) << 14  // bits 21..15
	pts |= int64(b[3]) << 7        // bits 14..7
	pts |= int64(b[4]&0xFE) >> 1   // bits 6..0

	return pts
}
