# ISSUE-002: External `ffprobe` Process Forking Per Segment

## 📌 Metadata
* **ID**: ISSUE-002
* **Component**: Worker Transcode Engine
* **File**: [`internal/worker/executor.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/worker/executor.go#L216-L227)
* **Category**: Compute / CPU Efficiency
* **Impact**: Medium (CPU Context Switching & Process Execution Overhead)

---

## 🔍 Description

After FFmpeg finishes transcoding a segment file (`.ts`), the worker invokes an external binary command `ffprobe` via `exec.Command` to extract the exact floating-point duration of the file.

### Code Snapshot
```go
// internal/worker/executor.go
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

## 💥 Resource Impact

* **Process Spawning Overhead**: Spawning a new OS process (`fork` + `execve` + binary load + dynamic linker resolving + process termination) requires thousands of CPU clock cycles per invocation.
* **Scale Inflation**: A batch job of 1,000 segments causes **1,000 separate `ffprobe` process spawns**, competing for CPU execution time and context switches with active FFmpeg transcode workers.

---

## 🛠️ Proposed Solution

FFmpeg already knows the exact duration of the segment it transcoded. Duration can be extracted either:
1. By parsing FFmpeg stderr/progress logs (e.g. `progress=end`, `out_time_us=...`).
2. By calculating fixed GOP duration (e.g. 5.0s for keyframe-aligned segments except the final chunk).

### Recommended Fix:
Capture FFmpeg output stdout/stderr or `-progress pipe:1` to retrieve `out_time_ms` directly from the primary transcode command execution, avoiding an external `ffprobe` process launch entirely.

---

## 📊 Expected Resource Gain
* Saves 100% of `ffprobe` process spawning overhead.
* Reduces CPU context switching on worker nodes.
