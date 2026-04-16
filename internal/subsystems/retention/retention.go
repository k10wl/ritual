// Package retention builds the retention service slice (local + remote
// backups + log retention) from settings and manifest rules.
package retention

import (
	"fmt"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
)

// Build returns the full retention slice used by the Retaining stage.
// remoteManifest supplies the remote retention rules; local rules come
// from the host settings file.
func Build(
	localStorage, remoteStorage ports.StorageRepository,
	bus ports.EventBus,
	remoteManifest *domain.Manifest,
) ([]ports.RetentionService, error) {
	parse := services.ChainStrategies(services.ParseTimestampDir, services.ParseTimestampTar)

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

	local, err := services.NewRetention(localStorage, localRules, "backups", parse)
	if err != nil {
		return nil, fmt.Errorf("local retention: %w", err)
	}
	remote, err := services.NewRetention(remoteStorage, remoteRules, "backups", parse)
	if err != nil {
		return nil, fmt.Errorf("remote retention: %w", err)
	}
	logRet, err := services.NewLogRetention(localStorage, bus)
	if err != nil {
		return nil, fmt.Errorf("log retention: %w", err)
	}

	return []ports.RetentionService{local, remote, logRet}, nil
}
