// Package conditions builds the pre-flight condition slice from the
// remote manifest thresholds and injected provider adapters.
package conditions

import (
	"fmt"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
)

// Build returns the ordered condition slice for the Checking stage.
// Provider interfaces are injected so tests can pass fakes and the
// package compiles on any OS.
func Build(
	remoteManifest *domain.Manifest,
	remoteManifests ports.ManifestStore,
	sys services.SystemInfoProvider,
	disk services.DiskInfoProvider,
	java services.JavaVersionProvider,
) ([]ports.ConditionService, error) {
	lock, err := services.NewManifestLockCondition(remoteManifests)
	if err != nil {
		return nil, fmt.Errorf("lock condition: %w", err)
	}
	ram, err := services.NewRAMCondition(remoteManifest.GetMinRAMMB(), sys)
	if err != nil {
		return nil, fmt.Errorf("ram condition: %w", err)
	}
	diskCond, err := services.NewDiskSpaceCondition(remoteManifest.GetMinDiskMB(), config.RootPath, disk)
	if err != nil {
		return nil, fmt.Errorf("disk condition: %w", err)
	}
	javaCond, err := services.NewJavaVersionCondition(remoteManifest.GetMinJavaVersion(), java)
	if err != nil {
		return nil, fmt.Errorf("java condition: %w", err)
	}

	return []ports.ConditionService{lock, ram, diskCond, javaCond}, nil
}
