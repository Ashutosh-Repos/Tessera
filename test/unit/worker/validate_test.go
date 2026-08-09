package worker_test

import (
	"bytes"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/distributed-transcoder/internal/worker"
	"github.com/distributed-transcoder/test/fixtures"
)

func TestExtractPTS_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		pts  int64
	}{
		{"zero", 0},
		{"one second", 90000},
		{"five seconds", 450000},
		{"large offset", 8589934591},
		{"near wrap", (1 << 33) - 100},
		{"max 33bit", (1 << 33) - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkt := fixtures.BuildTSPacket(true, tt.pts)
			got := worker.ExtractPTS(pkt[13:18])
			if got != tt.pts {
				t.Errorf("ExtractPTS() = %d, want %d", got, tt.pts)
			}
		})
	}
}

func TestProbeDurationGo_BasicDuration(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "segment_000_1080p.ts")

	// Generate 30 frames at 30 fps (PTS increment of 3000 per frame) = 1.0 second duration
	var ptsList []int64
	for i := 0; i < 30; i++ {
		ptsList = append(ptsList, int64(i*3000))
	}
	tsData := fixtures.BuildTSStream(ptsList)
	if err := os.WriteFile(tsPath, tsData, 0644); err != nil {
		t.Fatalf("failed to write test TS file: %v", err)
	}

	durStr := worker.ProbeDurationGo(tsPath)
	if durStr == "0" {
		t.Fatalf("ProbeDurationGo returned \"0\" on valid TS file")
	}

	// 29 intervals * 3000 ticks = 87000 ticks. frameDur = 3000. totalDuration = 90000 / 90000.0 = 1.000000s
	expected := "1.000000"
	if durStr != expected {
		t.Errorf("ProbeDurationGo() = %q, want %q", durStr, expected)
	}
}

func TestProbeDurationGo_CopyTsNonZeroStart(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "segment_003_1080p.ts")

	// Segment starts at PTS 1,350,000 (15 seconds offset) and runs 5 seconds (150 frames at 30fps)
	startPTS := int64(1350000)
	var ptsList []int64
	for i := 0; i < 150; i++ {
		ptsList = append(ptsList, startPTS+int64(i*3000))
	}
	tsData := fixtures.BuildTSStream(ptsList)
	if err := os.WriteFile(tsPath, tsData, 0644); err != nil {
		t.Fatalf("failed to write test TS file: %v", err)
	}

	durStr := worker.ProbeDurationGo(tsPath)
	expected := "5.000000"
	if durStr != expected {
		t.Errorf("ProbeDurationGo() = %q, want %q (copyts non-zero start must be normalized)", durStr, expected)
	}
}

func TestProbeDurationGo_ShortLastSegment(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "segment_010_1080p.ts")

	// Final segment of a video is only 2.3 seconds long (69 frames)
	var ptsList []int64
	for i := 0; i < 69; i++ {
		ptsList = append(ptsList, int64(i*3000))
	}
	tsData := fixtures.BuildTSStream(ptsList)
	if err := os.WriteFile(tsPath, tsData, 0644); err != nil {
		t.Fatalf("failed to write test TS file: %v", err)
	}

	durStr := worker.ProbeDurationGo(tsPath)
	expected := "2.300000"
	if durStr != expected {
		t.Errorf("ProbeDurationGo() = %q, want %q", durStr, expected)
	}
}

func TestProbeDurationGo_PTSWrapAround(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "segment_wrap.ts")

	const max33Bit = int64(1) << 33

	var ptsList []int64
	startPTS := max33Bit - 45000 // 0.5s before wrap
	for i := 0; i < 150; i++ {   // 5 seconds total (150 frames at 30fps)
		pts := (startPTS + int64(i*3000)) % max33Bit
		ptsList = append(ptsList, pts)
	}

	tsData := fixtures.BuildTSStream(ptsList)
	if err := os.WriteFile(tsPath, tsData, 0644); err != nil {
		t.Fatalf("failed to write test TS file: %v", err)
	}

	durStr := worker.ProbeDurationGo(tsPath)
	expected := "5.000000"
	if durStr != expected {
		t.Errorf("ProbeDurationGo() = %q, want %q (must seamlessly handle 33-bit PTS wrap-around)", durStr, expected)
	}
}

func TestProbeDurationGo_IgnoresAudioPTS(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "segment_av.ts")

	var buf []byte
	for i := 0; i < 150; i++ {
		videoPkt := fixtures.BuildTSPacket(true, int64(i*3000))
		buf = append(buf, videoPkt[:]...)

		audioPkt := fixtures.BuildAudioTSPacket(int64(i*3000 + 500))
		buf = append(buf, audioPkt[:]...)
	}

	if err := os.WriteFile(tsPath, buf, 0644); err != nil {
		t.Fatalf("failed to write test TS file: %v", err)
	}

	durStr := worker.ProbeDurationGo(tsPath)
	expected := "5.000000"
	if durStr != expected {
		t.Errorf("ProbeDurationGo() = %q, want %q (must filter out audio PES packets)", durStr, expected)
	}
}

func TestProbeDurationGo_WithAdaptationField(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "segment_adapt.ts")

	var buf []byte
	for i := 0; i < 150; i++ {
		pkt := fixtures.BuildTSPacketWithAdaptation(int64(i*3000), 10)
		buf = append(buf, pkt[:]...)
	}

	if err := os.WriteFile(tsPath, buf, 0644); err != nil {
		t.Fatalf("failed to write test TS file: %v", err)
	}

	durStr := worker.ProbeDurationGo(tsPath)
	expected := "5.000000"
	if durStr != expected {
		t.Errorf("ProbeDurationGo() = %q, want %q", durStr, expected)
	}
}

func TestProbeDurationGo_NonExistentFile(t *testing.T) {
	durStr := worker.ProbeDurationGo("/nonexistent/file.ts")
	if durStr != "0" {
		t.Errorf("ProbeDurationGo(nonexistent) = %q, want \"0\"", durStr)
	}
}

func TestProbeDurationGo_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "empty.ts")
	os.WriteFile(tsPath, []byte{}, 0644)

	durStr := worker.ProbeDurationGo(tsPath)
	if durStr != "0" {
		t.Errorf("ProbeDurationGo(empty) = %q, want \"0\"", durStr)
	}
}

func TestProbeDurationGo_WrongFileSize(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "badsize.ts")
	os.WriteFile(tsPath, []byte("short-unaligned-data"), 0644)

	durStr := worker.ProbeDurationGo(tsPath)
	if durStr != "0" {
		t.Errorf("ProbeDurationGo(bad size) = %q, want \"0\"", durStr)
	}
}

func TestProbeDurationGo_CorruptedSyncByte(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "badsync.ts")

	pkt := fixtures.BuildTSPacket(true, 0)
	pkt[0] = 0xFF // corrupt sync byte
	os.WriteFile(tsPath, pkt[:], 0644)

	durStr := worker.ProbeDurationGo(tsPath)
	if durStr != "0" {
		t.Errorf("ProbeDurationGo(corrupt sync) = %q, want \"0\"", durStr)
	}
}

func TestProbeDurationGo_NoPTSPackets(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "nopts.ts")

	pkt := fixtures.BuildTSPacket(false, 0)
	os.WriteFile(tsPath, pkt[:], 0644)

	durStr := worker.ProbeDurationGo(tsPath)
	if durStr != "0" {
		t.Errorf("ProbeDurationGo(no PTS) = %q, want \"0\"", durStr)
	}
}

func TestProbeDurationGo_SinglePTSPacket(t *testing.T) {
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "singlepts.ts")

	pkt := fixtures.BuildTSPacket(true, 90000)
	os.WriteFile(tsPath, pkt[:], 0644)

	durStr := worker.ProbeDurationGo(tsPath)
	if durStr != "0.000000" {
		t.Errorf("ProbeDurationGo(single frame) = %q, want \"0.000000\"", durStr)
	}
}

func TestProbeDurationGo_RealFFmpegFile(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed in environment, skipping real FFmpeg integration test")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed in environment, skipping real FFmpeg integration test")
	}

	dir := t.TempDir()
	tsPath := filepath.Join(dir, "real_ffmpeg.ts")

	cmd := exec.Command(ffmpegPath,
		"-f", "lavfi",
		"-i", "testsrc=duration=5:size=320x240:rate=30",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-g", "30",
		"-y", tsPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg command failed: %v\nOutput: %s", err, out)
	}

	probeCmd := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		tsPath,
	)
	probeOut, err := probeCmd.Output()
	if err != nil {
		t.Fatalf("ffprobe command failed: %v", err)
	}
	ffprobeDur := string(bytes.TrimSpace(probeOut))

	goDur := worker.ProbeDurationGo(tsPath)
	t.Logf("ffprobe duration: %s, probeDurationGo: %s", ffprobeDur, goDur)

	f1 := parseVal(ffprobeDur)
	f2 := parseVal(goDur)

	if math.Abs(f1-f2) > 0.05 {
		t.Errorf("probeDurationGo = %q (%f), ffprobe = %q (%f), diff exceeds 50ms tolerance", goDur, f2, ffprobeDur, f1)
	}
}

func parseVal(s string) float64 {
	var f float64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			f = f*10 + float64(c-'0')
		} else if c == '.' {
			break
		}
	}
	return f
}
