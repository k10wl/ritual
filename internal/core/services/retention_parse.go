package services

import (
	"path"
	"ritual/internal/config"
	"strings"
	"time"
)

// ParseStrategy extracts a UTC timestamp from a storage key.
// Returns zero time if key is not a recognized backup entry.
type ParseStrategy func(key string) time.Time

// ParseTimestampDir recognizes directory-format backups: {prefix}/{ts}/...
// Returns timestamp from the first path segment matching TimestampFormat.
func ParseTimestampDir(key string) time.Time {
	key = strings.TrimSuffix(key, "/")
	parts := strings.Split(key, "/")
	for _, p := range parts {
		if path.Ext(p) != "" {
			continue
		}
		if t, err := time.ParseInLocation(config.TimestampFormat, p, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ParseTimestampTar recognizes v1 tar-format backups: {prefix}/{ts}.tar
func ParseTimestampTar(key string) time.Time {
	base := path.Base(key)
	if path.Ext(base) != ".tar" {
		return time.Time{}
	}
	stem := strings.TrimSuffix(base, ".tar")
	t, err := time.ParseInLocation(config.TimestampFormat, stem, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ChainStrategies composes multiple parse strategies. First non-zero result wins.
func ChainStrategies(strategies ...ParseStrategy) ParseStrategy {
	return func(key string) time.Time {
		for _, s := range strategies {
			if t := s(key); !t.IsZero() {
				return t
			}
		}
		return time.Time{}
	}
}
