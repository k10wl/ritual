package services

import (
	"fmt"
	"slices"
	"time"

	"ritual/internal/core/domain"
)

// Mark classifies keys into a delete list per retention rules.
// Pure function. No IO. Unparseable keys are marked for deletion (sacred dir).
func Mark(keys []string, rules domain.RetentionRules, parse ParseStrategy) []string {
	type entry struct {
		key string
		t   time.Time
	}

	var parsed []entry
	var unparseable []string

	for _, k := range keys {
		t := parse(k)
		if t.IsZero() {
			unparseable = append(unparseable, k)
			continue
		}
		parsed = append(parsed, entry{key: k, t: t.UTC()})
	}

	// Newest first; deterministic tiebreaker by key
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

	// keep_last
	for i, e := range parsed {
		if i < rules.KeepLast {
			protected[e.key] = true
		}
	}

	// keep_daily: first entry seen per UTC calendar day, up to limit
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

	// keep_weekly: first per ISO week
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

	// keep_monthly: first per UTC calendar month
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
	toDelete = append(toDelete, unparseable...)
	for _, e := range parsed {
		if !protected[e.key] {
			toDelete = append(toDelete, e.key)
		}
	}

	return toDelete
}
