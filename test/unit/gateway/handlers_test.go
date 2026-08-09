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
