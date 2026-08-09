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

// ProbeDurationGo reads an MPEG-TS (.ts) file and computes its duration
// by extracting the first and last video Presentation Timestamps (PTS).
func ProbeDurationGo(filePath string) string {
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

	var minPTS int64 = -1
	var maxPTS int64 = -1
	var prevPTS int64 = -1
	var baseOffset int64 = 0
	var frameCount int64 = 0

	packet := make([]byte, tsPacketSize)

	for {
		_, err := io.ReadFull(f, packet)
		if err != nil {
			break
		}

		if packet[0] != tsSyncByte {
			return "0"
		}

		hasPUSI := (packet[1] & 0x40) != 0
		if !hasPUSI {
			continue
		}

		hasAdaptation := (packet[3] & 0x20) != 0
		payloadOffset := 4
		if hasAdaptation {
			adaptationLength := int(packet[4])
			payloadOffset = 5 + adaptationLength
		}

		if payloadOffset+13 >= tsPacketSize {
			continue
		}

		if packet[payloadOffset] != 0x00 || packet[payloadOffset+1] != 0x00 || packet[payloadOffset+2] != 0x01 {
			continue
		}

		streamID := packet[payloadOffset+3]
		if streamID < 0xE0 || streamID > 0xEF {
			continue
		}

		ptsDtsFlags := (packet[payloadOffset+7] & 0xC0) >> 6
		if ptsDtsFlags != 2 && ptsDtsFlags != 3 {
			continue
		}

		rawPTS := ExtractPTS(packet[payloadOffset+9 : payloadOffset+14])

		if prevPTS >= 0 {
			if rawPTS < prevPTS-0x10000000 {
				baseOffset += ptsMaxValue
			}
		}
		prevPTS = rawPTS
		unwrappedPTS := rawPTS + baseOffset

		if minPTS < 0 || unwrappedPTS < minPTS {
			minPTS = unwrappedPTS
		}
		if maxPTS < 0 || unwrappedPTS > maxPTS {
			maxPTS = unwrappedPTS
		}
		frameCount++
	}

	if frameCount == 0 || minPTS < 0 || maxPTS < 0 {
		return "0"
	}

	if frameCount == 1 {
		return "0.000000"
	}

	diff := maxPTS - minPTS
	frameDur := float64(diff) / float64(frameCount-1)
	totalDuration := (float64(diff) + frameDur) / ptsClockRate
	return fmt.Sprintf("%.6f", totalDuration)
}

// ExtractPTS decodes a 33-bit Presentation Timestamp from 5 bytes
func ExtractPTS(b []byte) int64 {
	_ = b[4]

	pts := int64(b[0]&0x0E) << 29
	pts |= int64(b[1]) << 22
	pts |= int64(b[2]&0xFE) << 14
	pts |= int64(b[3]) << 7
	pts |= int64(b[4]&0xFE) >> 1

	return pts
}
