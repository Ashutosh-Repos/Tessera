package gateway_test

import (
	"testing"

	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/internal/models"
)

func TestValidateUploadRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     models.CreateSessionRequest
		wantErr bool
	}{
		{
			name:    "valid 1GB upload",
			req:     models.CreateSessionRequest{FileSizeBytes: 1 * 1024 * 1024 * 1024, FileName: "test.mp4"},
			wantErr: false,
		},
		{
			name:    "valid max 50GB upload",
			req:     models.CreateSessionRequest{FileSizeBytes: 50 * 1024 * 1024 * 1024, FileName: "large.mp4"},
			wantErr: false,
		},
		{
			name:    "zero byte file size",
			req:     models.CreateSessionRequest{FileSizeBytes: 0, FileName: "empty.mp4"},
			wantErr: true,
		},
		{
			name:    "negative file size",
			req:     models.CreateSessionRequest{FileSizeBytes: -100, FileName: "invalid.mp4"},
			wantErr: true,
		},
		{
			name:    "exceeds 50GB max limit",
			req:     models.CreateSessionRequest{FileSizeBytes: 51 * 1024 * 1024 * 1024, FileName: "too_large.mp4"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gateway.ValidateUploadRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUploadRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeJobID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "full cluster key with job prefix and status suffix",
			input:    "job:{us-east-1:1234-5678}:status",
			expected: "us-east-1:1234-5678",
		},
		{
			name:     "key with only job prefix",
			input:    "job:{us-west-2:9999}",
			expected: "us-west-2:9999",
		},
		{
			name:     "key with only status suffix",
			input:    "eu-central-1:8888}:status",
			expected: "eu-central-1:8888",
		},
		{
			name:     "raw job ID without hash tags",
			input:    "ap-southeast-1:7777",
			expected: "ap-southeast-1:7777",
		},
		{
			name:     "empty key",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gateway.NormalizeJobID(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeJobID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCalculateTotalParts(t *testing.T) {
	partSize := int64(50 * 1024 * 1024) // 50MB
	tests := []struct {
		name     string
		fileSize int64
		expected int
	}{
		{"exact multiple", 100 * 1024 * 1024, 2},
		{"just under multiple", 100*1024*1024 - 1, 2},
		{"just over multiple", 100*1024*1024 + 1, 3},
		{"less than one part", 1024 * 1024, 1},
		{"zero size", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gateway.CalculateTotalParts(tt.fileSize, partSize)
			if got != tt.expected {
				t.Errorf("CalculateTotalParts(%d) = %d, want %d", tt.fileSize, got, tt.expected)
			}
		})
	}
}
