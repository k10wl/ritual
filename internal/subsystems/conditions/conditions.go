// Package conditions builds the pre-flight check slice from settings
// thresholds and injected provider adapters. The Checking stage iterates
// the returned slice; per-check observability is added via the Observed
// decorator at this composition site.
package conditions

import (
	"ritual/internal/adapters/observed"
	"ritual/internal/config"
	"ritual/internal/core/checks"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// HardwareInfoProvider aggregates the hardware providers needed by checks.
// Injected so tests can pass fakes and the package compiles on any OS.
type HardwareInfoProvider interface {
	checks.SystemInfoProvider
	checks.DiskInfoProvider
}

// Build returns the ordered, observed pre-flight check slice. Threshold
// values come from settings; observability events publish on bus.
func Build(
	s *domain.Settings,
	hw HardwareInfoProvider,
	java checks.JavaVersionProvider,
	bus ports.EventBus,
) []checks.Check {
	return []checks.Check{
		observed.NewCheck("ram", checks.RAM(s.MinRAMMB, hw), bus),
		observed.NewCheck("disk", checks.Disk(s.MinDiskMB, config.WorkRoot, hw), bus),
		observed.NewCheck("java", checks.Java(s.MinJavaVersion, java), bus),
	}
}
