package coordinator

import (
	"testing"
)

func TestIsFaststart(t *testing.T) {
	tests := []struct {
		name     string
		prefix   []byte
		expected bool
	}{
		{
			name:     "Typical faststart",
			prefix:   []byte("....ftypmp42....moov.....mdat...."),
			expected: true,
		},
		{
			name:     "Non-faststart (mdat first)",
			prefix:   []byte("....ftypmp42....mdat.....moov...."),
			expected: false,
		},
		{
			name:     "Fragmented/No mdat (moov only)",
			prefix:   []byte("....ftypmp42....moov...."),
			expected: true,
		},
		{
			name:     "Neither atom present",
			prefix:   []byte("....ftypmp42....free...."),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFaststart(tt.prefix)
			if result != tt.expected {
				t.Errorf("expected isFaststart=%t, got %t", tt.expected, result)
			}
		})
	}
}
