package retention

import (
	"context"
	"path"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
	"time"
)

const refsPrefix = "refs/"

// refsRetention selects ref documents (refs/{ts}.json) that fall outside the
// tiered keep policy. Parse is method-bound: the struct owns its keyspace
// contract end-to-end.
type refsRetention struct {
	storage ports.StorageRepository
	rules   domain.RetentionRules
}

// NewRefsRetention wires a Retention for the refs/ keyspace of the given
// storage. Storage is used only for List; deletion is the caller's job.
func NewRefsRetention(storage ports.StorageRepository, rules domain.RetentionRules) Retention {
	return &refsRetention{storage: storage, rules: rules}
}

// Select lists refs/ and returns the keys whose timestamps fall outside the
// policy. Returns nil when nothing is to be dropped.
func (r *refsRetention) Select(ctx context.Context) ([]string, error) {
	keys, err := r.storage.List(ctx, refsPrefix)
	if err != nil {
		return nil, err
	}
	return markKeys(keys, r.rules, r.parseTime), nil
}

// parseTime extracts the UTC timestamp from a refs/{ts}.json key.
// Returns zero time for any key not matching the refs layout; the engine
// treats zero-time as "not ours, skip".
func (r *refsRetention) parseTime(key string) time.Time {
	base := path.Base(key)
	if path.Ext(base) != ".json" {
		return time.Time{}
	}
	stem := strings.TrimSuffix(base, ".json")
	t, err := time.ParseInLocation(config.TimestampFormat, stem, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}
