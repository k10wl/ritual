// Package projection folds ports.EventBus events into a single GUI ViewModel
// and surfaces each new snapshot through an emit callback. The callback is
// the port — cmd/gui wires a Wails-typed event emitter; tests wire a slice
// accumulator. The projection has no Wails dependency.
package projection

import "fmt"

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
	// StagePreflight is the gray, inert autoupdate bucket shown before IDLE on
	// every launch (and on manual re-check) — the dial means "system working,
	// hands off". Carries PhasePreflight then PhaseUpdating. Design-log/037.
	StagePreflight Stage = "preflight"
	// StageRelocating is the workroot-relocate bucket (design-log/055
	// addendum): a settings-level operation, not a session flow, gated to
	// only run while the session dial is idle/failed. Own Stage rather than
	// reusing StageDownloading/StageUploading because it copies files
	// locally in neither the pull nor the push direction — a distinct dial
	// colour keeps that honest instead of implying network transfer.
	StageRelocating Stage = "relocating"
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
	// PhasePreflight: gray inert dial, "Checking for updates···" (autoupdate
	// probe in flight). PhaseUpdating: gray dial, "Updating → vN" while the new
	// binary downloads + the brief "Restarting···" tail before relaunch. Both
	// live in StagePreflight. Design-log/037 §State-machine additions.
	PhasePreflight Phase = "preflight"
	PhaseUpdating  Phase = "updating"
	// PhaseRelocating carries StageRelocating's whole duration (planning →
	// copying → verifying → committing) as one dial beat — file-count-driven
	// progress (FilesDone/FilesTotal, Progress) while copying; the frontend
	// reads FilesDone>=FilesTotal to know copying gave way to the tail
	// verify/commit work, the same "bytesDone>=bytesTotal ⇒ tail" pattern
	// PhaseSaving already uses. Design-log/055 addendum.
	PhaseRelocating Phase = "relocating"
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
	// Seq is a strictly increasing sequence number stamped once per emit by
	// Projection.emit (design-log/051 Q11). The Wails/WebView2 delivery of
	// each emit to the frontend is fire-and-forget and does not guarantee
	// execution order matches submission order under load; the frontend
	// compares Seq against the last-applied value and drops anything not
	// strictly greater, so a stale duplicate that finishes executing late
	// can never overwrite a newer snapshot already applied.
	Seq         int64   `json:"seq"`
	Stage       Stage   `json:"stage"`
	Phase       Phase   `json:"phase"`
	Progress    int     `json:"progress"`
	BytesDone   int64   `json:"bytesDone"`
	BytesTotal  int64   `json:"bytesTotal"`
	FilesDone   int     `json:"filesDone"`
	FilesTotal  int     `json:"filesTotal"`
	SpeedMbps   float64 `json:"speedMbps"`
	LogicalMbps float64 `json:"logicalMbps"`
	// EtaSeconds is the remaining transfer time during byte-flowing beats
	// (downloading / saving), computed Go-side from the beat-wide average rate
	// — bytes flowed since the beat began over elapsed since the beat began —
	// not the volatile 5-second rolling rate that SpeedMbps reports. Monotone
	// non-increasing within a beat (never climbs while bytes flow); re-baselines
	// on every stage change and PlanInfo. 0 means "no estimate yet" (first tick
	// of a beat, empty plan, or a non-transfer phase): the dial shows the
	// decoder placeholder rather than a fake number. The frontend renders this
	// directly — no division in JS. Design-log/028. Supersedes the old
	// remaining/effectiveSpeedBps division that inherited SpeedMbps's swing.
	EtaSeconds int64         `json:"etaSeconds"`
	ErrorText  string        `json:"errorText"`
	LockHolder string        `json:"lockHolder"`
	Addresses  []JoinAddress `json:"addresses"`

	// UptimeSeconds is elapsed time since PhasePlaying began, re-emitted once
	// a second by a dedicated ticker in Projection.Run (independent of the
	// progress.Ticker) so the frontend never runs its own clock — the value
	// only changes because the backend pushed a new one. 0 outside
	// PhasePlaying. The frontend formats it with the same formatEta() it
	// already uses for EtaSeconds: the backend drives *when* this changes,
	// the frontend still decides *how* to render it. Design-log/050.
	UptimeSeconds int64 `json:"uptimeSeconds"`

	// PrepEtaSeconds / WrapEtaSeconds are backend-ticked countdowns for the
	// two invisible session beats (design-log/058, superseding 027 §Q8's
	// static-ETA decision): PrepEtaSeconds counts down once a second while
	// Phase stays PhasePreparing, WrapEtaSeconds while Phase stays
	// PhaseWrapping — same "backend pushes, frontend only formats" pattern
	// as UptimeSeconds above, driven by the same Run ticker. Both seeded
	// from history-derived estimates (internal/subsystems/preprundup) at
	// beat entry and floored at 0; 0 outside the relevant phase or when no
	// history exists yet (frontend falls back to static copy).
	PrepEtaSeconds int64 `json:"prepEtaSeconds"`
	WrapEtaSeconds int64 `json:"wrapEtaSeconds"`

	// TargetVersion is the semver the autoupdate is moving to during
	// PhasePreflight/PhaseUpdating — the dial renders "Updating → v{TargetVersion}".
	// Empty outside the update flow. Design-log/037.
	TargetVersion string `json:"targetVersion"`

	// Stalled marks a byte-flowing beat (downloading / saving) whose link has
	// gone quiet mid-transfer — the latest tick carried a zero "now" rate while
	// bytes are still owed (BytesDone < BytesTotal). The frontend turns this
	// into "Stalled — waiting on R2…" so a multi-second R2 retransmit silence
	// reads as live-but-waiting rather than a dead-frozen dial. False once bytes
	// resume, at completion, and outside the transfer stages. Design-log/022 #2.
	Stalled bool `json:"stalled"`

	// SizeDoneText/SizeTotalText/SizeUnit are formatSize's output — Go's
	// single source of truth for converting BytesDone/BytesTotal into a
	// displayable magnitude ("58.8", "1.19", "GB"). Empty strings mean "no
	// plan yet"; the frontend's own bytesTotal>0 check decides whether to
	// show these or its decoder-placeholder animation — that threshold read
	// and the ring-fill ratio (bytesDone/bytesTotal) stay frontend concerns.
	// Design-log/050.
	SizeDoneText  string `json:"sizeDoneText"`
	SizeTotalText string `json:"sizeTotalText"`
	SizeUnit      string `json:"sizeUnit"`

	// SpeedText/SpeedUnit are formatSpeed's output, computed from
	// LogicalMbps (the decompress/install rate — matches what the dial has
	// always displayed, design-log/018) converted to bytes/s. "0"/"B/s"
	// while no rate is available yet; the frontend's own logicalMbps<=0
	// check (not this text) decides whether to show it or a placeholder.
	// Design-log/050.
	SpeedText string `json:"speedText"`
	SpeedUnit string `json:"speedUnit"`
}

// Snap is published to the EventBus after every ViewModel change so
// subscribers (e.g. the file logger) can record the computed GUI state —
// EtaSeconds in particular is not visible in the raw progress.Tick events.
type Snap struct{ ViewModel }

func (s Snap) String() string {
	vm := s.ViewModel
	return fmt.Sprintf(
		"[snap] stage=%s phase=%s progress=%d%% eta=%ds done=%d/%d speed=%.2fMbps stalled=%v prepEta=%ds wrapEta=%ds",
		vm.Stage, vm.Phase, vm.Progress, vm.EtaSeconds, vm.BytesDone, vm.BytesTotal, vm.SpeedMbps, vm.Stalled,
		vm.PrepEtaSeconds, vm.WrapEtaSeconds,
	)
}
