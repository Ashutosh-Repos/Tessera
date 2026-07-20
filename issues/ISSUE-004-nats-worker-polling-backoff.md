# ISSUE-004: Artificial 100ms Polling Latency in Worker Task Puller

## 📌 Metadata
* **ID**: ISSUE-004
* **Component**: Worker Task Dispatcher
* **File**: [`internal/worker/daemon.go`](file:///Users/ashutoshkumar/Desktop/Apple%20Project/internal/worker/daemon.go#L138-L164)
* **Category**: Latency & Queue Efficiency
* **Impact**: Low-Medium (Task Pick-up Delay Under Low/Burst Load)

---

## 🔍 Description

In `internal/worker/daemon.go`, the worker `taskPuller` routine polls the NATS message queue. If no messages are returned or if the buffer channel is full, it sleeps for a hardcoded `100ms` backoff duration.

### Code Snapshot
```go
// internal/worker/daemon.go
msgs, err := w.bus.PullTasks(ctx, shard, 10)
if len(msgs) == 0 {
    // No messages available — backoff to avoid CPU spin
    select {
    case <-ctx.Done():
        return
    case <-time.After(100 * time.Millisecond):
    }
    continue
}
```

---

## 💥 Resource Impact

* **Artificial Latency**: When a worker finishes a batch and becomes idle, it waits up to `100ms` before checking for incoming segment tasks, introducing artificial tail latency between task execution cycles.
* **Lower Duty-Cycle Density**: Under short segment durations (e.g. 2s segments running on GPU in ~200ms), a 100ms delay consumes up to 33% of active duty cycle.

---

## 🛠️ Proposed Solution

Utilize NATS JetStream blocking pull subscriptions (`PullSubscribe` with `Fetch(batch, nats.Context(ctx))`) or exponential adaptive backoff (e.g., 5ms $\rightarrow$ 10ms $\rightarrow$ 50ms) instead of a fixed 100ms sleep loop.

---

## 📊 Expected Resource Gain
* Eliminates up to 100ms queue fetch delay per task batch.
* Increases GPU/CPU worker utilization density under burst processing workloads.
