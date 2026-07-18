package selfupdate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrefixFor(t *testing.T) {
	assert.Equal(t, "bin/windows-amd64/", PrefixFor("windows", "amd64"))
	assert.Equal(t, "bin/linux-arm64/", PrefixFor("linux", "arm64"))
}

func TestLatest_PicksHighestSemver(t *testing.T) {
	const prefix = "bin/windows-amd64/"
	keys := []string{
		prefix + "2.0.0/aaa.exe",
		prefix + "2.10.0/ccc.exe", // newer than 2.9 despite lexical order
		prefix + "2.9.0/bbb.exe",
	}
	got := latest(prefix, keys)
	assert.Equal(t, "2.10.0", got.Version)
	assert.Equal(t, prefix+"2.10.0/ccc.exe", got.Key)
	assert.Equal(t, "ccc", got.SHA256, "leaf basename is the checksum, .exe stripped")
}

func TestLatest_StripsExtensionForSha_AndKeepsFullKey(t *testing.T) {
	const prefix = "bin/linux-amd64/"
	keys := []string{prefix + "2.1.0/9f86d0deadbeef"} // no extension (non-windows)
	got := latest(prefix, keys)
	assert.Equal(t, "2.1.0", got.Version)
	assert.Equal(t, "9f86d0deadbeef", got.SHA256)
	assert.Equal(t, prefix+"2.1.0/9f86d0deadbeef", got.Key)
}

func TestLatest_IgnoresMalformedAndForeignKeys(t *testing.T) {
	const prefix = "bin/windows-amd64/"
	keys := []string{
		prefix + "2.1.0/good.exe",         // valid
		prefix + "windows-amd64",          // wrong depth — no version dir
		prefix + "2.2.0/sub/dir/too.deep", // wrong depth — 3 segments
		prefix + "notaversion/x.exe",      // version segment isn't version-like
		"bin/linux-amd64/9.9.9/other.exe", // not under the queried prefix
	}
	got := latest(prefix, keys)
	assert.Equal(t, "2.1.0", got.Version, "only the well-formed in-prefix key wins; a foreign-OS 9.9.9 must not leak in")
	assert.Equal(t, prefix+"2.1.0/good.exe", got.Key)
}

func TestLatest_EmptyListIsZeroUpdate(t *testing.T) {
	got := latest("bin/windows-amd64/", nil)
	assert.Equal(t, "", got.Version, "no keys → zero Update → caller treats as 'nothing to update to'")
}
