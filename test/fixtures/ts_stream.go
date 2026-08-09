package fixtures

// BuildTSPacket constructs a single 188-byte MPEG-TS packet with:
//   - sync byte 0x47
//   - PUSI (payload unit start indicator) set if hasPES is true
//   - adaptation_field_control = payload only (0x01)
//   - PES header with video stream ID and PTS if hasPES is true
func BuildTSPacket(hasPES bool, pts int64) [188]byte {
	var pkt [188]byte
	pkt[0] = 0x47

	if !hasPES {
		pkt[3] = 0x10
		return pkt
	}

	pkt[1] = 0x40
	pkt[3] = 0x10

	pkt[4] = 0x00
	pkt[5] = 0x00
	pkt[6] = 0x01
	pkt[7] = 0xE0 // video
	pkt[8] = 0x00
	pkt[9] = 0x00
	pkt[10] = 0x80
	pkt[11] = 0x80
	pkt[12] = 0x05

	EncodePTS(pkt[13:18], pts)
	return pkt
}

func BuildTSPacketWithAdaptation(pts int64, adaptLen int) [188]byte {
	var pkt [188]byte
	pkt[0] = 0x47
	pkt[1] = 0x40
	pkt[3] = 0x30
	pkt[4] = byte(adaptLen)
	offset := 5 + adaptLen
	pkt[offset] = 0x00
	pkt[offset+1] = 0x00
	pkt[offset+2] = 0x01
	pkt[offset+3] = 0xE0
	pkt[offset+7] = 0x80
	EncodePTS(pkt[offset+9:offset+14], pts)
	return pkt
}

// BuildAudioTSPacket constructs a packet with audio stream ID (0xC0).
func BuildAudioTSPacket(pts int64) [188]byte {
	pkt := BuildTSPacket(true, pts)
	pkt[7] = 0xC0
	return pkt
}

// BuildTSStream generates an in-memory MPEG-TS byte slice with packets containing the given PTS sequence.
func BuildTSStream(ptsSequence []int64) []byte {
	var b []byte
	for _, pts := range ptsSequence {
		pkt := BuildTSPacket(true, pts)
		b = append(b, pkt[:]...)
	}
	return b
}

func EncodePTS(buf []byte, pts int64) {
	buf[0] = byte(((pts >> 30) & 0x07) << 1) | 0x21
	buf[1] = byte((pts >> 22) & 0xFF)
	buf[2] = byte(((pts >> 15) & 0x7F) << 1) | 0x01
	buf[3] = byte((pts >> 7) & 0xFF)
	buf[4] = byte((pts & 0x7F) << 1) | 0x01
}
