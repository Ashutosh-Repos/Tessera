package infra_test

import (
	"strings"
	"testing"

	"github.com/distributed-transcoder/internal/infra"
)

func TestRedisKeys_HashTagging(t *testing.T) {
	jobID := "us-east:7574a6cb-4b9b-4b06-9749-89135111cdbf"
	keys := infra.NewRedisKeys(jobID)

	expectedHashTag := "{" + jobID + "}"

	keyList := []struct {
		name string
		val  string
	}{
		{"StatusHash", keys.StatusHash()},
		{"ProgressBitmap", keys.ProgressBitmap()},
		{"DurationsHash", keys.DurationsHash()},
		{"ManifestCache", keys.ManifestCache()},
		{"ProgressStream", keys.ProgressStream()},
	}

	for _, k := range keyList {
		t.Run(k.name, func(t *testing.T) {
			if !strings.Contains(k.val, expectedHashTag) {
				t.Errorf("%s = %q does not contain HashTag %q (required for Redis Cluster CROSSSLOT safety)", k.name, k.val, expectedHashTag)
			}
		})
	}
}
