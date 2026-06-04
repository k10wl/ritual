package retention

import (
	"context"
	"fmt"
	"path"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
	"time"
)

const refsPrefix = "refs/"

// Scope selects which side's rules a refs retention reads from the settings file
// at prune time (design-log/039). Local and remote rules are independent knobs
// in domain.Settings; the same engine serves both, picking by Scope.
type Scope int

const (
	// ScopeLocal reads domain.Settings.LocalRetention.
	ScopeLocal Scope = iota
	// ScopeRemote reads domain.Settings.RemoteRetention.
	ScopeRemote
)

// refsRetention selects ref documents (refs/{ts}.json) that fall outside the
// tiered keep policy. Rules are NOT captured at construction: they are read from
// the settings file inside Select, at prune time, so a GUI edit takes effect on
// the next sync without an app restart (design-log/039 §Q1 — user directive: read
// settings when we need them, no closures). Parse is method-bound: the struct
// owns its keyspace contract end-to-end.
type refsRetention struct {
	storage ports.StorageRepository
	scope   Scope
}

// NewRefsRetention wires a Retention for the refs/ keyspace of the given
// storage. scope picks which settings field (local/remote) Select reads at prune
// time. Storage is used only for List; deletion is the caller's job.
func NewRefsRetention(storage ports.StorageRepository, scope Scope) Retention {
	return &refsRetention{storage: storage, scope: scope}
}

// Select reads the current rules from the settings file, lists refs/, and
// returns the keys whose timestamps fall outside the policy. Reading settings
// here (not at construction) is the whole point of design-log/039: edits apply
// to the next prune without a restart. A zero-value rule set (unconfigured)
// falls back to defaults, preserving pre-039 behaviour. Returns nil when nothing
// is to be dropped.
func (r *refsRetention) Select(ctx context.Context) ([]string, error) {
	settings, err := domain.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("retention load settings: %w", err)
	}
	rules := settings.LocalRetention
	if r.scope == ScopeRemote {
		rules = settings.RemoteRetention
	}
	if rules == (domain.RetentionRules{}) {
		rules = domain.DefaultRetentionRules()
	}
	keys, err := r.storage.List(ctx, refsPrefix)
	if err != nil {
		return nil, err
	}
	return markKeys(keys, rules, r.parseTime), nil
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
