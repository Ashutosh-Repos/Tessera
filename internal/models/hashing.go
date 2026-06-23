package models

import "hash/fnv"

// PartitionOf deterministically maps a Job UUID to a partition.
// Used by both Gateway (to set the S3 path prefix) and Coordinator
// (to validate incoming events belong to an owned partition).
// Algorithm: FNV-1a of the raw job UUID string, mod totalPartitions.
func PartitionOf(jobID string, totalPartitions int) int {
	h := fnv.New32a()
	h.Write([]byte(jobID))
	return int(h.Sum32()) % totalPartitions
}
