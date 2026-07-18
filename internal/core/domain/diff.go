// Package domain holds the pure business entities shared across core services.
package domain

import "sort"

// DiffResult contains the sets of files that differ between two file maps.
// Upload = files changed or new in local vs remote.
// Download = files changed or new in remote vs local.
// Delete = files present in remote but absent in local.
type DiffResult struct {
	Upload   []string
	Download []string
	Delete   []string
}

// ComputeDiff compares local and remote file maps by content hash and produces
// three sorted sets. Pure function — no IO, no side effects. Size differences
// alone do not produce a diff entry.
func ComputeDiff(local, remote map[string]FileEntry) DiffResult {
	var result DiffResult

	for path, localEntry := range local {
		remoteEntry, exists := remote[path]
		if !exists || localEntry.Hash != remoteEntry.Hash {
			result.Upload = append(result.Upload, path)
		}
	}

	for path, remoteEntry := range remote {
		localEntry, exists := local[path]
		if !exists {
			result.Download = append(result.Download, path)
			result.Delete = append(result.Delete, path)
		} else if remoteEntry.Hash != localEntry.Hash {
			result.Download = append(result.Download, path)
		}
	}

	sort.Strings(result.Upload)
	sort.Strings(result.Download)
	sort.Strings(result.Delete)

	return result
}
