package coordinator_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/distributed-transcoder/internal/coordinator"
)

func TestHashRing_RebuildAndOwnerOf(t *testing.T) {
	ring := coordinator.NewHashRing()

	ring.Rebuild([]string{"coord-node-1"})

	for p := 0; p < 1024; p++ {
		owner := ring.OwnerOf(p)
		if owner != "coord-node-1" {
			t.Errorf("OwnerOf(%d) = %q, want coord-node-1", p, owner)
		}
	}

	owned := ring.OwnedPartitions("coord-node-1", 1024)
	if len(owned) != 1024 {
		t.Errorf("len(OwnedPartitions) = %d, want 1024", len(owned))
	}
}

func TestHashRing_PartitionDistribution(t *testing.T) {
	ring := coordinator.NewHashRing()
	nodes := []string{"node-a", "node-b", "node-c"}

	ring.Rebuild(nodes)
	totalPartitions := 1024
	counts := make(map[string]int)

	for p := 0; p < totalPartitions; p++ {
		owner := ring.OwnerOf(p)
		if owner == "" {
			t.Errorf("OwnerOf(%d) = empty string, want active node", p)
		}
		counts[owner]++
	}

	for _, node := range nodes {
		cnt := counts[node]
		if cnt < 250 || cnt > 450 {
			t.Errorf("node %s owned %d partitions, expected balanced distribution between 250 and 450", node, cnt)
		}
	}
}

func TestHashRing_EmptyRing(t *testing.T) {
	ring := coordinator.NewHashRing()
	ring.Rebuild([]string{})

	owner := ring.OwnerOf(10)
	if owner != "" {
		t.Errorf("OwnerOf(10) on empty ring = %q, want empty string", owner)
	}

	owned := ring.OwnedPartitions("node-1", 1024)
	if len(owned) != 0 {
		t.Errorf("OwnedPartitions on empty ring = %v, want empty slice", owned)
	}
}

func TestHashRing_ConcurrentReadRebuild(t *testing.T) {
	ring := coordinator.NewHashRing()
	ring.Rebuild([]string{"node-1", "node-2"})

	var wg sync.WaitGroup
	numGoroutines := 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = ring.OwnerOf(j % 1024)
				_ = ring.OwnedPartitions(fmt.Sprintf("node-%d", (id%2)+1), 1024)
			}
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			if j%2 == 0 {
				ring.Rebuild([]string{"node-1", "node-2", "node-3"})
			} else {
				ring.Rebuild([]string{"node-1", "node-2"})
			}
		}
	}()

	wg.Wait()
}

func TestHashRing_Members(t *testing.T) {
	ring := coordinator.NewHashRing()
	nodes := []string{"node-1", "node-2", "node-3"}
	ring.Rebuild(nodes)

	members := ring.Members()
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}

	// Verify it's a copy
	members[0] = "mutated"
	members2 := ring.Members()
	if members2[0] == "mutated" {
		t.Errorf("Members() returned a reference to the internal slice, expected a copy")
	}
}
