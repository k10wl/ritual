package services

import (
	"strconv"
	"strings"
)

// IsVersionOlder reports whether local is older than remote using
// numeric semver-style comparison. Sole consumers — services.migration
// and services.validator — are themselves scheduled for deletion in
// Tasks 18/19; this file dies with them.
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

func parseVersion(version string) []int {
	var parts []int
	for _, part := range strings.Split(version, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			n = 0
		}
		parts = append(parts, n)
	}
	return parts
}
