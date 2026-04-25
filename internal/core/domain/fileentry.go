package domain

// FileEntry pairs a file's content hash with its size in bytes.
// Hash is used for change detection; Size feeds plan/progress events.
type FileEntry struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}
