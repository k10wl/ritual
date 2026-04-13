package domain

import "sort"

// DiffResult contains the sets of files that differ between two xxhash maps.
// Upload = files changed or new in local vs remote.
// Download = files changed or new in remote vs local.
// Delete = files present in remote but absent in local.
type DiffResult struct {
	Upload   []string
	Download []string
	Delete   []string
}

// ComputeDiff compares local and remote xxhash maps and produces three sorted sets.
// Pure function — no IO, no side effects.
func ComputeDiff(local, remote map[string]string) DiffResult {
	var result DiffResult

	for path, localHash := range local {
		remoteHash, exists := remote[path]
		if !exists || localHash != remoteHash {
			result.Upload = append(result.Upload, path)
		}
	}

	for path, remoteHash := range remote {
		localHash, exists := local[path]
		if !exists {
			result.Download = append(result.Download, path)
			result.Delete = append(result.Delete, path)
		} else if remoteHash != localHash {
			result.Download = append(result.Download, path)
		}
	}

	sort.Strings(result.Upload)
	sort.Strings(result.Download)
	sort.Strings(result.Delete)

	return result
}
