package selfupdate

import (
	"path"
	"ritual/internal/core/ports"
	"strings"
)

// PrefixFor builds the per-platform listing prefix, e.g.
// "bin/windows-amd64/". The client lists only its own platform so it never
// sees (or picks) another OS/arch's artifacts (design-log/037 §Q7).
func PrefixFor(goos, goarch string) string {
	return "bin/" + goos + "-" + goarch + "/"
}

// latest scans the keys returned by List(prefix) and returns the highest-
// semver artifact as a ports.Update. Each key is
// "<prefix><version>/<sha256>[.exe]"; the version is the path segment under
// the prefix and the sha256 is the leaf's basename — integrity is intrinsic
// to the key, so no sidecar or feed file is read (design-log/037 §Q7).
//
// Keys that don't fit the shape (wrong depth, non-version segment) are
// ignored, so a stray object under the prefix can't masquerade as a release.
// Returns the zero Update when nothing valid is found; the caller treats an
// empty Version as "nothing to update to".
func latest(prefix string, keys []string) ports.Update {
	var best ports.Update
	for _, key := range keys {
		rel := strings.TrimPrefix(key, prefix)
		if rel == key {
			continue // not under the queried prefix
		}
		segs := strings.Split(strings.Trim(rel, "/"), "/")
		if len(segs) != 2 {
			continue // expect exactly <version>/<leaf>
		}
		version, leaf := segs[0], segs[1]
		if !isVersionLike(version) || leaf == "" {
			continue
		}
		sha := strings.TrimSuffix(leaf, path.Ext(leaf)) // drop ".exe" etc.
		if sha == "" {
			continue
		}
		if best.Version == "" || IsVersionOlder(best.Version, version) {
			best = ports.Update{Version: version, Key: key, SHA256: sha}
		}
	}
	return best
}

// isVersionLike accepts only digit-and-dot strings with at least one digit, so
// a directory like "windows-amd64" or "" never parses as a (degenerate, very
// low) version that latest would otherwise consider.
func isVersionLike(s string) bool {
	if s == "" {
		return false
	}
	hasDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '.':
		default:
			return false
		}
	}
	return hasDigit
}
