//go:build windows

// Package conditions builds the pre-flight condition slice from the
// remote manifest thresholds.
package conditions

import (
	"fmt"

	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/services"
)

// Build returns the ordered condition slice for the Checking stage.
// remoteManifest supplies thresholds; remoteManifests is used for the
// lock-check condition.
func Build(remoteManifest *domain.Manifest, remoteManifests ports.ManifestStore) ([]ports.ConditionService, error) {
	sys := adapters.NewWindowsSystemInfo()
	java := adapters.NewJavaInfo()

	lock, err := services.NewManifestLockCondition(remoteManifests)
	if err != nil {
		return nil, fmt.Errorf("lock condition: %w", err)
	}
	ram, err := services.NewRAMCondition(remoteManifest.GetMinRAMMB(), sys)
	if err != nil {
		return nil, fmt.Errorf("ram condition: %w", err)
	}
	disk, err := services.NewDiskSpaceCondition(remoteManifest.GetMinDiskMB(), config.RootPath, sys)
	if err != nil {
		return nil, fmt.Errorf("disk condition: %w", err)
	}
	javaVer, err := services.NewJavaVersionCondition(remoteManifest.GetMinJavaVersion(), java)
	if err != nil {
		return nil, fmt.Errorf("java condition: %w", err)
	}

	return []ports.ConditionService{lock, ram, disk, javaVer}, nil
}
