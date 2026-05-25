// Package projection folds ports.EventBus events into a single GUI ViewModel
// and surfaces each new snapshot through an emit callback. The callback is
// the port — cmd/gui wires a Wails-typed event emitter; tests wire a slice
// accumulator. The projection has no Wails dependency.
package projection

// Stage is the coarse UI bucket the dial colour reads from. Values map to
// the four 007-defined dial colours (plus locked + failed overlays).
//
// Stage is intentionally less granular than Phase: Stage drives the dial's
// outer ring colour and the gross bucket the user perceives ("downloading"
// vs "playing" vs "saving"). Phase drives the inner-glyph + sub-copy +
// ETA-visibility within each bucket. See design-log/017.
type Stage string

// Stage values. Keep JSON-stable: TypeScript matches on these exact strings.
// Lock-held conflicts route through StageFailed with vm.LockHolder set —
// design-log/017 folded the prior StageLocked into the Failed bucket so the
// app has a single failure pathway.
const (
	StageIdle        Stage = "idle"
	StageDownloading Stage = "downloading"
	StageRunning     Stage = "running"
	StageUploading   Stage = "uploading"
	StageFailed      Stage = "failed"
)

// Phase is the finer-grained sub-state the frontend uses to pick glyph,
// sub-copy, and ETA visibility within a Stage bucket. Eight phases collapse
// the eight runtime ritual stages plus the server lifecycle into beats the
// user actually perceives. Per design-log/017:
//
//   - downloading: bytes flowing in (Pulling[download]). ETA visible.
//   - preparing:   invisible work (Pulling[apply] + Acquiring + Running before
//     ServerReady). ETA hidden. Dial sub: "Preparing…".
//   - playing:     server reachable (Running after ServerReadyInfo).
//   - wrapping:    server stopping + Committing (post hold-stop). ETA hidden.
//     Dial sub: "Wrapping up…".
//   - saving:      bytes flowing out (Pushing). ETA visible. After Pushing
//     completes (Unlocking + Retaining), still saving but with empty sub —
//     frontend detects via bytesDone>=bytesTotal.
//   - locked:      lock holder reported during Acquiring. Idle overlay.
//   - failed:      terminal until DismissRequested clears it.
//   - idle:        nothing running.
type Phase string

// Phase values. Keep JSON-stable: TypeScript matches on these exact strings.
// PhaseLocked was folded into PhaseFailed in design-log/017: lock conflicts
// surface as a Failed beat with vm.LockHolder populated so the frontend
// renders the friendly "{holder} is playing" title.
const (
	PhaseIdle        Phase = "idle"
	PhaseDownloading Phase = "downloading"
	PhasePreparing   Phase = "preparing"
	PhasePlaying     Phase = "playing"
	PhaseWrapping    Phase = "wrapping"
	PhaseSaving      Phase = "saving"
	PhaseFailed      Phase = "failed"
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
//
// BytesDone / BytesTotal are logical (uncompressed) bytes, so the progress
// bar percentage is internally consistent. SpeedMbps is the wire-layer rate
// (5-second rolling average, matches curl's --progress-bar) — units differ
// from the bar deliberately (see design-log/001-progress-projection.md
// §"What this means for the size estimate" + §Refinement).
//
// LogicalMbps is the rate at which logical bytes flow through the caller
// layer — Steam's "green bar" (decompress / install rate), distinct from
// SpeedMbps which is the network throughput. Both numbers are computed
// server-side and shipped pre-derived so a chart component reads them
// directly without parsing strings or differencing successive snapshots.
//
// Per design-log/017 the projection no longer ships a per-substage Label
// string: the dial reads `Phase` and looks up its copy locally. Stage-level
// strings like "Snapshotting…" were dev/log copy and never reached the user.
type ViewModel struct {
	Stage       Stage         `json:"stage"`
	Phase       Phase         `json:"phase"`
	Progress    int           `json:"progress"`
	BytesDone   int64         `json:"bytesDone"`
	BytesTotal  int64         `json:"bytesTotal"`
	FilesDone   int           `json:"filesDone"`
	FilesTotal  int           `json:"filesTotal"`
	SpeedMbps   float64       `json:"speedMbps"`
	LogicalMbps float64       `json:"logicalMbps"`
	ErrorText   string        `json:"errorText"`
	LockHolder  string        `json:"lockHolder"`
	Addresses   []JoinAddress `json:"addresses"`
}
