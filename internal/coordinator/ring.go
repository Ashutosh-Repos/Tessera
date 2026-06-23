package coordinator

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

const virtualNodesPerCoordinator = 150 // for balanced distribution

type HashRing struct {
	mu      sync.RWMutex
	ring    []uint32          // sorted list of virtual node hashes
	nodeMap map[uint32]string // hash → coordinator node ID
	members []string          // active coordinator IDs
}

func NewHashRing() *HashRing {
	return &HashRing{
		nodeMap: make(map[uint32]string),
	}
}

// Rebuild recalculates the ring from the current set of active coordinators.
// Called on every etcd watch event (coordinator join/leave).
func (hr *HashRing) Rebuild(activeNodes []string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.ring = hr.ring[:0]
	hr.nodeMap = make(map[uint32]string)
	hr.members = activeNodes

	for _, nodeID := range activeNodes {
		for i := 0; i < virtualNodesPerCoordinator; i++ {
			key := fmt.Sprintf("%s#%d", nodeID, i)
			h := fnv.New32a()
			h.Write([]byte(key))
			hash := h.Sum32()
			hr.ring = append(hr.ring, hash)
			hr.nodeMap[hash] = nodeID
		}
	}
	sort.Slice(hr.ring, func(i, j int) bool { return hr.ring[i] < hr.ring[j] })
}

// OwnerOf returns the coordinator node ID that owns a given partition.
func (hr *HashRing) OwnerOf(partitionID int) string {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	if len(hr.ring) == 0 {
		return ""
	}

	key := fmt.Sprintf("partition:%d", partitionID)
	h := fnv.New32a()
	h.Write([]byte(key))
	hash := h.Sum32()

	// Binary search for the first virtual node >= hash
	idx := sort.Search(len(hr.ring), func(i int) bool { return hr.ring[i] >= hash })
	if idx == len(hr.ring) {
		idx = 0 // wrap around
	}
	return hr.nodeMap[hr.ring[idx]]
}

// OwnedPartitions returns all partition IDs owned by a specific node.
func (hr *HashRing) OwnedPartitions(nodeID string, totalPartitions int) []int {
	var owned []int
	for p := 0; p < totalPartitions; p++ {
		if hr.OwnerOf(p) == nodeID {
			owned = append(owned, p)
		}
	}
	return owned
}
