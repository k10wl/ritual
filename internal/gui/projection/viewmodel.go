// Package projection folds ports.EventBus events into a single GUI ViewModel
// and surfaces each new snapshot through an emit callback. The callback is
// the port — cmd/gui wires a Wails-typed event emitter; tests wire a slice
// accumulator. The projection has no Wails dependency.
package projection

// Stage is the coarse UI screen the frontend renders.
// Values map 1:1 to stage components in frontend/src/stages.
type Stage string

// Stage values. Keep JSON-stable: TypeScript matches on these exact strings.
const (
	StageIdle        Stage = "idle"
	StageDownloading Stage = "downloading"
	StageRunning     Stage = "running"
	StageUploading   Stage = "uploading"
	StageLocked      Stage = "locked"
	StageFailed      Stage = "failed"
)

// JoinAddress pairs a human label with a dial address shown on the Running
// screen so non-technical users can copy and share a reachable endpoint.
type JoinAddress struct {
	Label   string `json:"label"`
	Address string `json:"address"`
}

// ViewModel is the full payload the main window renders from.
// Every field is safe to read even when it is not relevant to the current
// stage — the frontend decides which subset to display per stage.
type ViewModel struct {
	Stage      Stage         `json:"stage"`
	Progress   int           `json:"progress"`
	BytesDone  int64         `json:"bytesDone"`
	BytesTotal int64         `json:"bytesTotal"`
	FilesDone  int           `json:"filesDone"`
	FilesTotal int           `json:"filesTotal"`
	Label      string        `json:"label"`
	ErrorText  string        `json:"errorText"`
	LockHolder string        `json:"lockHolder"`
	ReadyLight bool          `json:"readyLight"`
	Addresses  []JoinAddress `json:"addresses"`
}
