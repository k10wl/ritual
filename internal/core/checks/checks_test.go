package checks_test

import (
	"errors"
	"ritual/internal/core/checks"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSysInfo struct {
	freeRAM int
	err     error
}

func (f fakeSysInfo) GetFreeRAMMB() (int, error) { return f.freeRAM, f.err }

type fakeDiskInfo struct {
	freePerPath map[string]int
	err         error
}

func (f fakeDiskInfo) GetFreeDiskMB(path string) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.freePerPath[path], nil
}

type fakeJavaInfo struct {
	version int
	err     error
}

func (f fakeJavaInfo) GetJavaVersion() (int, error) { return f.version, f.err }

func TestRAM_PassesWhenHostMeetsConfiguredMinimum(t *testing.T) {
	check := checks.RAM(4096, fakeSysInfo{freeRAM: 8192})

	require.NoError(t, check(t.Context()),
		"RAM check must accept a host that has more free memory than the configured threshold")
}

func TestRAM_FailsWithSentinelWhenHostBelowMinimum(t *testing.T) {
	check := checks.RAM(4096, fakeSysInfo{freeRAM: 1024})

	err := check(t.Context())

	require.Error(t, err, "RAM check must reject a host below the configured minimum")
	assert.ErrorIs(t, err, checks.ErrInsufficientRAM,
		"RAM check failure must wrap ErrInsufficientRAM so callers can branch on the sentinel")
	assert.Contains(t, err.Error(), "1024",
		"RAM check error must surface the actual free RAM so operators can see the gap")
	assert.Contains(t, err.Error(), "4096",
		"RAM check error must surface the configured threshold so operators can see the gap")
}

func TestRAM_PropagatesProviderErrorContext(t *testing.T) {
	check := checks.RAM(4096, fakeSysInfo{err: errors.New("perfmon unavailable")})

	err := check(t.Context())

	require.Error(t, err, "RAM check must surface provider failures rather than silently passing")
	assert.Contains(t, err.Error(), "perfmon unavailable",
		"RAM check error must include the underlying provider error so root causes survive the wrapping")
}

func TestDisk_PassesWhenVolumeMeetsConfiguredMinimum(t *testing.T) {
	check := checks.Disk(5120, "C:\\ritual", fakeDiskInfo{freePerPath: map[string]int{"C:\\ritual": 20480}})

	require.NoError(t, check(t.Context()),
		"Disk check must accept a volume with more free space than the configured threshold")
}

func TestDisk_FailsWithSentinelWhenVolumeBelowMinimum(t *testing.T) {
	check := checks.Disk(5120, "C:\\ritual", fakeDiskInfo{freePerPath: map[string]int{"C:\\ritual": 1024}})

	err := check(t.Context())

	require.Error(t, err, "Disk check must reject a volume below the configured minimum")
	assert.ErrorIs(t, err, checks.ErrInsufficientDisk,
		"Disk check failure must wrap ErrInsufficientDisk so callers can branch on the sentinel")
	assert.Contains(t, err.Error(), "C:\\ritual",
		"Disk check error must include the path so operators know which volume to clear")
}

func TestJava_PassesWhenInstalledVersionMeetsMinimum(t *testing.T) {
	check := checks.Java(21, fakeJavaInfo{version: 21})

	require.NoError(t, check(t.Context()),
		"Java check must accept the host when the installed major version meets the configured minimum")
}

func TestJava_FailsWithVersionTooOldSentinelWhenBelowMinimum(t *testing.T) {
	check := checks.Java(21, fakeJavaInfo{version: 17})

	err := check(t.Context())

	require.Error(t, err, "Java check must reject a host whose installed Java is older than the minimum")
	assert.ErrorIs(t, err, checks.ErrJavaVersionTooOld,
		"Java check failure must wrap ErrJavaVersionTooOld so callers can branch on the sentinel")
}

func TestJava_FailsWithNotFoundSentinelWhenProviderErrors(t *testing.T) {
	check := checks.Java(21, fakeJavaInfo{err: errors.New("java not on PATH")})

	err := check(t.Context())

	require.Error(t, err, "Java check must reject the host when the provider cannot detect Java")
	assert.ErrorIs(t, err, checks.ErrJavaNotFound,
		"Java check provider failure must wrap ErrJavaNotFound so callers can branch on the sentinel")
}
