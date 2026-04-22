package services

import (
	"context"
	"path"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
	"time"
)

// logsRetention selects log files (logs/{ts}.log) that fall outside the
// tiered keep policy. Local-only: logs never leave the host.
type logsRetention struct {
	storage ports.StorageRepository
	rules   domain.RetentionRules
}

// NewLogsRetention wires a Retention for the logs/ keyspace of the given
// storage. Typically constructed with RetentionRules{KeepLast: N}.
func NewLogsRetention(storage ports.StorageRepository, rules domain.RetentionRules) Retention {
	return &logsRetention{storage: storage, rules: rules}
}

// Select lists logs/ and returns the keys whose timestamps fall outside the
// policy.
func (r *logsRetention) Select(ctx context.Context) ([]string, error) {
	keys, err := r.storage.List(ctx, config.LogsDir)
	if err != nil {
		return nil, err
	}
	return markKeys(keys, r.rules, r.parseTime), nil
}

// parseTime extracts the UTC timestamp from a logs/{ts}.log key.
// Returns zero time for any key not matching the logs layout.
func (r *logsRetention) parseTime(key string) time.Time {
	base := path.Base(key)
	if path.Ext(base) != config.LogExtension {
		return time.Time{}
	}
	stem := strings.TrimSuffix(base, config.LogExtension)
	t, err := time.ParseInLocation(config.TimestampFormat, stem, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}
