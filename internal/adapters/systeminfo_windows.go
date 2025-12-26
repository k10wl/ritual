package adapters

import (
	"ritual/internal/core/services"
	"syscall"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// memoryStatusEx corresponds to Windows MEMORYSTATUSEX structure
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// WindowsSystemInfo provides system information on Windows
type WindowsSystemInfo struct{}

// Compile-time checks to ensure WindowsSystemInfo implements the required interfaces
var _ services.SystemInfoProvider = (*WindowsSystemInfo)(nil)
var _ services.DiskInfoProvider = (*WindowsSystemInfo)(nil)

// NewWindowsSystemInfo creates a new WindowsSystemInfo instance
func NewWindowsSystemInfo() *WindowsSystemInfo {
	return &WindowsSystemInfo{}
}

// GetFreeRAMMB returns the available free RAM in megabytes
func (w *WindowsSystemInfo) GetFreeRAMMB() (int, error) {
	var memStatus memoryStatusEx
	memStatus.Length = uint32(unsafe.Sizeof(memStatus))

	ret, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memStatus)))
	if ret == 0 {
		return 0, err
	}

	// Convert bytes to megabytes
	return int(memStatus.AvailPhys / (1024 * 1024)), nil
}

// GetFreeDiskMB returns the available free disk space in megabytes for the given path
func (w *WindowsSystemInfo) GetFreeDiskMB(path string) (int, error) {
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	ret, _, callErr := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 {
		return 0, callErr
	}

	// Convert bytes to megabytes
	return int(freeBytesAvailable / (1024 * 1024)), nil
}
