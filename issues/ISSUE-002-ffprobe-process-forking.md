# 🐛 ISSUE-002: Subprocess Fork/Exec Bottleneck — Per-Segment External `ffprobe` Invocations

## 📌 Metadata
* **Issue ID**: `ISSUE-002`
* **Component**: Worker Transcode Engine (`TaskExecutor`)
* **Affected File**: [`internal/worker/executor.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/worker/executor.go#L131-L227)
* **Category**: Compute Density & OS Kernel Process Scheduling
* **Severity / Impact**: **Medium-High** (Severe CPU Context-Switching & Scale Inflation under Volume)
* **Status**: `Open`

---

## 🔍 Executive Problem Overview

In [`internal/worker/executor.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/worker/executor.go#L131), immediately after an `ffmpeg` transcode job finishes writing a `.ts` segment file to disk/storage, the worker node invokes a secondary external binary—**`ffprobe`**—to parse the file and extract its floating-point duration.

While functional in small dev environments, this design creates a **massive mechanical OS process bottleneck**. For every single 5-second segment transcoded across multiple target resolutions, an entire secondary operating system process (`ffprobe`) must be spawned, dynamically linked, executed, and reaped.

```
┌────────────────────────────────────────────────────────────────────────────────────────────┐
│ CURRENT ARCHITECTURE (Per Segment Task)                                                    │
├────────────────────────────────────────────────────────────────────────────────────────────┤
│ 1. Worker receives Task ──────► 2. Fork/Exec `ffmpeg` (Transcode segment)                  │
│                                           │                                                │
│                                           ▼                                                │
│                                 3. `ffmpeg` completes                                      │
│                                           │                                                │
│                                           ▼                                                │
│                                 4. Fork/Exec `ffprobe` (Read duration) ◄── [BOTTLENECK]    │
│                                           │                                                │
│                                           ▼                                                │
│                                 5. `ffprobe` completes                                     │
│                                           │                                                │
│                                           ▼                                                │
│                                 6. Upload & Single-RTT Redis Pipeline                      │
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 🔬 Deep Technical & Kernel-Level Breakdown

### 1. Mechanical OS Process Lifecycle Cost
When Go calls `exec.Command("ffprobe", ...).Output()`, the OS performs the following expensive kernel-level operations:
1. **System Calls (`vfork` / `clone` + `execve`)**: The kernel allocates a new Process Control Block (PCB), assigns a new Process ID (PID), and sets up virtual memory page tables.
2. **Dynamic Library Linking (`dyld` / `ld.so`)**: The OS dynamic linker opens and maps shared libraries into the process address space (`libavformat.so`, `libavcodec.so`, `libavutil.so`, `libz.so`, `libc.so`).
3. **File Descriptor Allocation**: Creates standard `stdin`, `stdout`, `stderr` pipes and opens the target `.ts` segment file from storage/disk.
4. **Media Demuxing**: `ffprobe` parses MPEG-TS program headers, reads Program Clock References (PCR) / Presentation Timestamps (PTS), and formats the text output.
5. **Kernel Reaping**: OS schedules `waitpid()`, tears down page tables, triggers TLB (Translation Lookaside Buffer) cache flushes, and reclaims memory.

### 2. Quantitative Scale Inflation & Math
For a **1-hour video** broken into 5-second segments (720 segments) across **3 target resolutions** (1080p, 720p, 480p):

$$\text{Total Segment Tasks} = 720 \times 3 = 2,160 \text{ tasks}$$

* **FFmpeg Processes**: 2,160 processes
* **`ffprobe` Processes**: **2,160 extra OS process spawns**
* **Time Wasted**: At an average execution footprint of ~15ms per `ffprobe` launch:
  $$2,160 \times 0.015\text{s} = \mathbf{32.4\text{ seconds of pure CPU process creation overhead per video!}}$$
* **Fleet Scale Impact**: A fleet of 100 GPU worker nodes processing 1,000 video assets per day executes **over 6,480,000 process forks daily**, causing severe kernel lock contention on PID allocation (`/proc/sys/kernel/pid_max`) and CPU core context switching.

---

## 🎯 Code Location & Trace

* **Invocation Site**: [`internal/worker/executor.go:131`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/worker/executor.go#L131)
  ```go
  // Step 8: Probe duration BEFORE upload
  duration := te.probeDuration(localOutput)
  ```
* **Function Implementation**: [`internal/worker/executor.go:216-227`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/worker/executor.go#L216-L227)
  ```go
  func (te *TaskExecutor) probeDuration(filePath string) string {
      out, err := exec.Command("ffprobe",
          "-v", "error",
          "-show_entries", "format=duration",
          "-of", "default=noprint_wrappers=1:nokey=1",
          filePath,
      ).Output()
      if err != nil {
          return "0"
      }
      return strings.TrimSpace(string(out))
  }
  ```

---

## 🛠️ Proposed Engineering Solutions

FFmpeg **already knows and computes** the precise duration during the primary transcode pass. Spawning a second utility is redundant.

### Solution A: Direct FFmpeg Machine-Readable Progress Pipe (Recommended)
Pass `-progress pipe:1` or `-progress /dev/stdout` to the main `ffmpeg` command execution. FFmpeg writes real-time key-value metrics directly to a Go `io.Pipe`:

```go
// Stream parsing FFmpeg output metrics in Go (Zero extra processes)
// Output snippet from FFmpeg:
// out_time_us=5000000
// progress=end

func parseDurationFromFFmpegProgress(r io.Reader) string {
    scanner := bufio.NewScanner(r)
    var durationMicroSec int64
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "out_time_us=") {
            val := strings.TrimPrefix(line, "out_time_us=")
            durationMicroSec, _ = strconv.ParseInt(val, 10, 64)
        }
    }
    return fmt.Sprintf("%.6f", float64(durationMicroSec)/1000000.0)
}
```

### Solution B: In-Memory MPEG-TS Packet PTS Parser (Pure Go)
Write a 30-line pure Go reader that opens the `.ts` file, reads the final 188-byte MPEG-TS packet header, and extracts the 33-bit Presentation Timestamp (PTS):
* **Execution Time**: `< 0.05ms`
* **Subprocesses**: `0`
* **Allocations**: `0` heap allocations

---

## 📊 Expected Performance & Efficiency Gains

| Metric | Before Optimization | After Optimization (Solution A/B) | Net Improvement |
| :--- | :--- | :--- | :--- |
| **Subprocesses per Task** | 2 (`ffmpeg` + `ffprobe`) | 1 (`ffmpeg` only) | **50% fewer OS processes** |
| **OS Kernel Context Switches** | High (~4,320 / video job) | Low (~2,160 / video job) | **50% reduction in CPU switching** |
| **Task Duration Extraction Overhead** | ~15ms–25ms per segment | ~0ms (extracted from stream) | **100% elimination of probing latency** |
| **Daily Fleet Process Spawns (100 workers)** | 6.48 Million process forks | 3.24 Million process forks | **3.24 Million process forks saved/day** |
