package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "v1 tar backup",
			key:      "world_backups/20260414103000.tar",
			expected: "20260414103000",
		},
		{
			name:     "v2 directory backup",
			key:      "world_backups/20260414103000",
			expected: "20260414103000",
		},
		{
			name:     "bare timestamp",
			key:      "20260414103000",
			expected: "20260414103000",
		},
		{
			name:     "tar.gz extension",
			key:      "world_backups/20260414103000.tar.gz",
			expected: "20260414103000",
		},
		{
			name:     "invalid name",
			key:      "world_backups/manual.tar",
			expected: "",
		},
		{
			name:     "temp file",
			key:      "world_backups/temp_something",
			expected: "",
		},
		{
			name:     "random string",
			key:      "world_backups/notadate",
			expected: "",
		},
		{
			name:     "empty string",
			key:      "",
			expected: "",
		},
		{
			name:     "partial timestamp",
			key:      "world_backups/2026041410",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTimestamp(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractTimestamp_BackwardsCompat_MixedFormats(t *testing.T) {
	// v1 .tar files and v2 directories both have valid timestamps
	entries := []string{
		"world_backups/20260414100000.tar", // v1 format
		"world_backups/20260414110000",      // v2 format (directory)
		"world_backups/20260414120000.tar", // v1 format
		"world_backups/20260414130000",      // v2 format
	}

	validCount := 0
	for _, entry := range entries {
		if extractTimestamp(entry) != "" {
			validCount++
		}
	}

	assert.Equal(t, 4, validCount, "all entries should be recognized regardless of format")
}
