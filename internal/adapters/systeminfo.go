package adapters

import (
	"ritual/internal/core/services"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// SystemInfo provides RAM and disk metrics via gopsutil. Cross-platform
// replacement for the former WindowsSystemInfo syscall adapter.
type SystemInfo struct{}

var (
	_ services.SystemInfoProvider = (*SystemInfo)(nil)
	_ services.DiskInfoProvider   = (*SystemInfo)(nil)
)

// NewSystemInfo returns a SystemInfo adapter.
func NewSystemInfo() *SystemInfo { return &SystemInfo{} }

// GetFreeRAMMB returns available RAM in megabytes.
func (*SystemInfo) GetFreeRAMMB() (int, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	mb := v.Available / (1024 * 1024)
	if mb > uint64(^uint(0)>>1) {
		mb = uint64(^uint(0) >> 1)
	}
	return int(mb), nil //nolint:gosec // clamped above
}

// GetFreeDiskMB returns free disk space at path in megabytes.
func (*SystemInfo) GetFreeDiskMB(path string) (int, error) {
	u, err := disk.Usage(path)
	if err != nil {
		return 0, err
	}
	mb := u.Free / (1024 * 1024)
	if mb > uint64(^uint(0)>>1) {
		mb = uint64(^uint(0) >> 1)
	}
	return int(mb), nil //nolint:gosec // clamped above
}
