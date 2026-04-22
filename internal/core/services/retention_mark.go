package services

import (
	"fmt"
	"ritual/internal/core/domain"
	"slices"
	"time"
)

// markKeys classifies keys into a delete list per retention rules.
// Pure function. No IO. Keys whose parse returns zero-time are skipped —
// unparseable is never destructive; the protocol keyspace may legitimately
// contain non-timestamp entries that retention must leave alone.
//
//nolint:gocyclo // tier classification branches per rule type; that is the spec.
func markKeys(keys []string, rules domain.RetentionRules, parse func(string) time.Time) []string {
	type entry struct {
		key string
		t   time.Time
	}

	parsed := make([]entry, 0, len(keys))
	for _, k := range keys {
		t := parse(k)
		if t.IsZero() {
			continue
		}
		parsed = append(parsed, entry{key: k, t: t.UTC()})
	}

	slices.SortFunc(parsed, func(a, b entry) int {
		if c := b.t.Compare(a.t); c != 0 {
			return c
		}
		if a.key < b.key {
			return -1
		}
		if a.key > b.key {
			return 1
		}
		return 0
	})

	protected := make(map[string]bool, len(parsed))

	for i, e := range parsed {
		if i < rules.KeepLast {
			protected[e.key] = true
		}
	}

	daySeen := map[string]bool{}
	dayCount := 0
	for _, e := range parsed {
		bucket := e.t.Format("2006-01-02")
		if !daySeen[bucket] && dayCount < rules.KeepDaily {
			protected[e.key] = true
			dayCount++
		}
		daySeen[bucket] = true
	}

	weekSeen := map[string]bool{}
	weekCount := 0
	for _, e := range parsed {
		y, w := e.t.ISOWeek()
		bucket := fmt.Sprintf("%d-W%02d", y, w)
		if !weekSeen[bucket] && weekCount < rules.KeepWeekly {
			protected[e.key] = true
			weekCount++
		}
		weekSeen[bucket] = true
	}

	monthSeen := map[string]bool{}
	monthCount := 0
	for _, e := range parsed {
		bucket := e.t.Format("2006-01")
		if !monthSeen[bucket] && monthCount < rules.KeepMonthly {
			protected[e.key] = true
			monthCount++
		}
		monthSeen[bucket] = true
	}

	var toDelete []string
	for _, e := range parsed {
		if !protected[e.key] {
			toDelete = append(toDelete, e.key)
		}
	}
	return toDelete
}
