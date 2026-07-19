// Package selfupdate restores Ritual's self-update, lost in 046045a when the
// v1 manifest tail was removed (design-log/037). The byte-level replace +
// checksum + rollback is delegated to github.com/minio/selfupdate; this
// package owns the version decision, the listing-derived feed, and relaunch.
package selfupdate

import (
	"strconv"
	"strings"
)

// IsVersionOlder reports whether local is an older semver than remote.
// Compares major.minor.patch numerically (e.g. "1.2.3"); when every shared
// part is equal the shorter version is the older one ("1.0" < "1.0.1").
// Lifted verbatim from the deleted updater_ritual.go (design-log/037 §A) —
// pure, no manifest dependency.
func IsVersionOlder(local, remote string) bool {
	localParts := parseVersion(local)
	remoteParts := parseVersion(remote)

	for i := 0; i < len(localParts) && i < len(remoteParts); i++ {
		if localParts[i] < remoteParts[i] {
			return true
		}
		if localParts[i] > remoteParts[i] {
			return false
		}
	}

	return len(localParts) < len(remoteParts)
}

// parseVersion splits "1.2.3" into [1, 2, 3]. Non-numeric parts read as 0 so a
// malformed key never panics — it just compares as the smallest version.
func parseVersion(version string) []int {
	var parts []int
	for part := range strings.SplitSeq(version, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			n = 0
		}
		parts = append(parts, n)
	}
	return parts
}
