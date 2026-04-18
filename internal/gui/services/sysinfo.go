package services

import "github.com/shirou/gopsutil/v4/mem"

// SysInfoService is a Wails service exposing host resource metrics.
type SysInfoService struct{}

// RAM describes host memory usage in megabytes and percent used.
type RAM struct {
	TotalMB     uint64  `json:"totalMB"`
	UsedMB      uint64  `json:"usedMB"`
	FreeMB      uint64  `json:"freeMB"`
	UsedPercent float64 `json:"usedPercent"`
}

// GetRAM returns current host memory stats.
func (*SysInfoService) GetRAM() (RAM, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return RAM{}, err
	}
	const mb = 1024 * 1024
	return RAM{
		TotalMB:     v.Total / mb,
		UsedMB:      v.Used / mb,
		FreeMB:      v.Available / mb,
		UsedPercent: v.UsedPercent,
	}, nil
}
