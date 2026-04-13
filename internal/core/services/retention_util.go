package services

import (
	"path/filepath"
	"ritual/internal/config"
	"strings"
	"time"
)

// extractTimestamp extracts a valid timestamp from a backup entry name.
// Returns the timestamp string if valid, empty string otherwise.
// Works with both file names ("20260414103000.tar") and directory names ("20260414103000").
func extractTimestamp(key string) string {
	base := filepath.Base(key)

	// Strip known extensions (.tar, .tar.gz, etc.)
	for {
		ext := filepath.Ext(base)
		if ext == "" {
			break
		}
		base = strings.TrimSuffix(base, ext)
	}

	// Validate as timestamp
	if _, err := time.Parse(config.TimestampFormat, base); err == nil {
		return base
	}

	return ""
}
