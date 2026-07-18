// Package retention builds the retention + GC Job slices used by the
// Retaining stage. Composition root for refs-side retention + GC and
// logs-side retention. Local and remote slices mirror the spec's
// commit→pruneLocal→push→pruneRemote pairing (§2297-2309); each side
// emits typed Retention*/GC* events keyed by Label.
package retention

import (
	"ritual/internal/adapters/observed"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	coreret "ritual/internal/core/retention"
	"ritual/internal/core/stages/retaining"
)

// Build wires the retention Jobs split per side. Local jobs cover
// refs-local (retention + GC) + logs-local; remote jobs cover refs-remote
// (retention + GC). Order within a side matters: retention drops manifests
// first, then GC mark-sweeps the orphan blobs they exposed.
//
// Refs retention reads its rules from the settings file at prune time, not here
// (design-log/039): Build only fixes the scope (local/remote) per side, so a GUI
// edit to the rules takes effect on the next sync without an app restart. Logs
// retention is not user-facing — it keeps a fixed by-value rule
// (config.MaxLogFiles).
func Build(
	localStorage, remoteStorage ports.StorageRepository,
	bus ports.EventBus,
) (local, remote []retaining.Job) {
	logRules := domain.RetentionRules{KeepLast: config.MaxLogFiles}

	local = []retaining.Job{
		retaining.NewRetentionRefsJob(
			"refs-local",
			observed.NewRetention(coreret.NewRefsRetention(localStorage, coreret.ScopeLocal), bus, "refs-local"),
			localStorage,
		),
		retaining.NewGCRefsJob(
			"gc-refs-local",
			refs.NewCollector(localStorage),
		),
		retaining.NewLogsJob(
			"logs-local",
			observed.NewRetention(coreret.NewLogsRetention(localStorage, logRules), bus, "logs-local"),
			localStorage,
		),
	}
	remote = []retaining.Job{
		retaining.NewRetentionRefsJob(
			"refs-remote",
			observed.NewRetention(coreret.NewRefsRetention(remoteStorage, coreret.ScopeRemote), bus, "refs-remote"),
			remoteStorage,
		),
		retaining.NewGCRefsJob(
			"gc-refs-remote",
			refs.NewCollector(remoteStorage),
		),
	}
	return local, remote
}
