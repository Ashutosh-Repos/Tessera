package models

import (
	"fmt"
	"testing"
)

func TestPartitionOf(t *testing.T) {
	totalPartitions := 1024

	// Test case 1: Deterministic mapping
	jobID1 := "7574a6cb-4b9b-4b06-9749-89135111cdbf"
	p1 := PartitionOf(jobID1, totalPartitions)
	p2 := PartitionOf(jobID1, totalPartitions)

	if p1 != p2 {
		t.Errorf("PartitionOf is non-deterministic: p1=%d, p2=%d", p1, p2)
	}

	// Test case 2: Bound checks
	if p1 < 0 || p1 >= totalPartitions {
		t.Errorf("PartitionOf returned out-of-bounds partition: %d", p1)
	}

	// Test case 3: Distribution
	partitionsSeen := make(map[int]bool)
	for i := 0; i < 100; i++ {
		jobID := fmt.Sprintf("job-uuid-%d", i)
		p := PartitionOf(jobID, totalPartitions)
		if p < 0 || p >= totalPartitions {
			t.Errorf("PartitionOf returned out-of-bounds partition %d for %s", p, jobID)
		}
		partitionsSeen[p] = true
	}

	// Check that we got multiple unique partitions (not all mapping to same slot)
	if len(partitionsSeen) <= 1 {
		t.Errorf("hashing distribution failure: only saw %d unique partitions", len(partitionsSeen))
	}
}
