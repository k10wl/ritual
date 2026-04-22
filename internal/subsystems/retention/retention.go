// Package retention builds the retention Job slice used by the Retaining
// stage. Composition root for refs-side retention+GC and logs-side
// retention.
package retention

import (
	"fmt"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/refs"
	"ritual/internal/core/services"
	"ritual/internal/core/stages/retaining"
)

// Build wires the retention Jobs: refs-local, refs-remote, logs-local.
// remoteManifest supplies the remote retention rules; local rules come from
// the host settings file. Each refs Job runs Select → DeleteBatch → Collect;
// the logs Job runs Select → DeleteBatch.
func Build(
	localStorage, remoteStorage ports.StorageRepository,
	bus ports.EventBus,
	remoteManifest *domain.Manifest,
) ([]retaining.Job, error) {
	settings, err := domain.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}

	localRules := settings.LocalRetention
	if localRules == (domain.RetentionRules{}) {
		localRules = domain.DefaultRetentionRules()
	}
	remoteRules := remoteManifest.RemoteRetention
	if remoteRules == (domain.RetentionRules{}) {
		remoteRules = domain.DefaultRetentionRules()
	}
	logRules := domain.RetentionRules{KeepLast: config.MaxLogFiles}

	return []retaining.Job{
		retaining.NewRefsJob(
			services.NewObservedRetention(services.NewRefsRetention(localStorage, localRules), bus, "refs-local"),
			localStorage,
			refs.NewCollector(localStorage),
		),
		retaining.NewRefsJob(
			services.NewObservedRetention(services.NewRefsRetention(remoteStorage, remoteRules), bus, "refs-remote"),
			remoteStorage,
			refs.NewCollector(remoteStorage),
		),
		retaining.NewLogsJob(
			services.NewObservedRetention(services.NewLogsRetention(localStorage, logRules), bus, "logs"),
			localStorage,
		),
	}, nil
}
