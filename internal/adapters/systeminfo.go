package adapters

import (
	"ritual/internal/core/services"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// SystemInfo provides RAM and disk metrics via gopsutil. Cross-platform
// replacement for the former WindowsSystemInfo syscall adapter.
type SystemInfo struct{}

var _ services.SystemInfoProvider = (*SystemInfo)(nil)
var _ services.DiskInfoProvider = (*SystemInfo)(nil)

func NewSystemInfo() *SystemInfo { return &SystemInfo{} }

func (*SystemInfo) GetFreeRAMMB() (int, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return int(v.Available / (1024 * 1024)), nil
}

func (*SystemInfo) GetFreeDiskMB(path string) (int, error) {
	u, err := disk.Usage(path)
	if err != nil {
		return 0, err
	}
	return int(u.Free / (1024 * 1024)), nil
}
