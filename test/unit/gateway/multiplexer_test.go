package gateway_test

import (
	"sync"
	"testing"

	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/internal/models"
)

func TestProgressMultiplexer_SubscribeUnsubscribe(t *testing.T) {
	pm := gateway.NewProgressMultiplexer(nil, 1000)

	ch1 := make(chan models.ProgressUpdate, 10)
	ch2 := make(chan models.ProgressUpdate, 10)
	jobID := "test-job-123"

	if count := pm.ActiveSubscriberCount(); count != 0 {
		t.Errorf("initial subscriber count = %d, want 0", count)
	}

	pm.Subscribe(jobID, ch1)
	pm.Subscribe(jobID, ch2)

	if count := pm.ActiveSubscriberCount(); count != 2 {
		t.Errorf("subscriber count after subscribe = %d, want 2", count)
	}

	pm.Unsubscribe(jobID, ch1)

	if count := pm.ActiveSubscriberCount(); count != 1 {
		t.Errorf("subscriber count after unsubscribe 1 = %d, want 1", count)
	}

	pm.Unsubscribe(jobID, ch2)

	if count := pm.ActiveSubscriberCount(); count != 0 {
		t.Errorf("subscriber count after unsubscribe 2 = %d, want 0", count)
	}
}

func TestProgressMultiplexer_Concurrency(t *testing.T) {
	pm := gateway.NewProgressMultiplexer(nil, 1000)
	var wg sync.WaitGroup

	numGoroutines := 50
	jobID := "concurrent-job"

	channels := make([]chan models.ProgressUpdate, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		channels[i] = make(chan models.ProgressUpdate, 1)
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pm.Subscribe(jobID, channels[idx])
		}(i)
	}
	wg.Wait()

	if count := pm.ActiveSubscriberCount(); count != numGoroutines {
		t.Errorf("concurrent subscriber count = %d, want %d", count, numGoroutines)
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pm.Unsubscribe(jobID, channels[idx])
		}(i)
	}
	wg.Wait()

	if count := pm.ActiveSubscriberCount(); count != 0 {
		t.Errorf("subscriber count after concurrent unsubscribe = %d, want 0", count)
	}
}
