package s3_integrity_checks

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func init() {
	// Disable logging during tests
	InfoLogger.SetOutput(io.Discard)
	DebugLogger.SetOutput(io.Discard)
}

func TestComputeCRC32(t *testing.T) {
	data := []byte("Hello, World")
	checksum := ComputeCRC32(data)
	assert.NotEmpty(t, checksum)
}

func TestUploadPhaseGetSummary(t *testing.T) {
	partNum := int32(1)
	tests := []struct {
		name     string
		phase    UploadPhase
		expected string
	}{
		{
			name: "successful phase",
			phase: UploadPhase{
				Stage:      Init,
				Success:    true,
				Message:    "test message",
				PartNumber: 0,
			},
			expected: "✓ upload initialization: test message",
		},
		{
			name: "failed phase with part number",
			phase: UploadPhase{
				Stage:      PartUpload,
				Success:    false,
				Message:    "failed",
				PartNumber: partNum,
				Error:      assert.AnError,
			},
			expected: "✗ part upload (Part 1): failed (assert.AnError general error for testing)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.phase.GetSummary()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMultipartUploadFailure(t *testing.T) {
	ctx := context.Background()
	input := MultipartUploadInput{
		Bucket:      "INVALID_BUCKET_NAME",
		Key:         "test/file.txt",
		Data:        []byte("test data"),
		EndpointURL: "http://localhost:4566", // LocalStack endpoint
		Region:      "us-east-1",
		Verbose:     false,
	}

	status, err := MultipartUpload(ctx, input)
	assert.Error(t, err)
	assert.NotNil(t, status)
	assert.Len(t, status.Phases, 1)
	assert.Equal(t, Init, status.Phases[0].Stage)
	assert.False(t, status.Phases[0].Success)
}
