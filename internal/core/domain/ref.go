package domain

// RefID canonical form: dash-separated, UTC, millisecond precision, 24 chars.
// Lexical order == chronological. No colons (Windows filename safe).
type RefID string

// RefIDFormat is the canonical time layout for RefID strings.
const RefIDFormat = "2006-01-02T15-04-05.000Z"

// Object is a content hash paired with the raw (pre-compression) size.
type Object struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// Ref is one content-addressed snapshot. Self-contained.
type Ref struct {
	Timestamp     RefID             `json:"timestamp"`
	Parent        RefID             `json:"parent,omitempty"`
	RitualVersion string            `json:"ritual_version"`
	Targets       []string          `json:"targets"`
	Objects       map[string]Object `json:"objects"`
}
