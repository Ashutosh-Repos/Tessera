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

	hash := hashPartitionKey(partitionID)

	// Binary search for the first virtual node >= hash
	idx := sort.Search(len(hr.ring), func(i int) bool { return hr.ring[i] >= hash })
	if idx == len(hr.ring) {
		idx = 0 // wrap around
	}
	return hr.nodeMap[hr.ring[idx]]
}

// hashPartitionKey computes FNV-1a 32-bit hash for "partition:<id>" without heap allocations.
func hashPartitionKey(partitionID int) uint32 {
	const offset32 uint32 = 2166136261
	const prime32 uint32 = 16777619

	h := offset32
	prefix := "partition:"
	for i := 0; i < len(prefix); i++ {
		h ^= uint32(prefix[i])
		h *= prime32
	}

	var buf [20]byte
	pos := len(buf)
	n := partitionID
	if n == 0 {
		pos--
		buf[pos] = '0'
	} else {
		neg := false
		if n < 0 {
			neg = true
			n = -n
		}
		for n > 0 {
			pos--
			buf[pos] = byte('0' + n%10)
			n /= 10
		}
		if neg {
			pos--
			buf[pos] = '-'
		}
	}

	for i := pos; i < len(buf); i++ {
		h ^= uint32(buf[i])
		h *= prime32
	}
	return h
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

// Members returns a thread-safe copy of the active coordinator node IDs.
func (hr *HashRing) Members() []string {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	members := make([]string, len(hr.members))
	copy(members, hr.members)
	return members
}
