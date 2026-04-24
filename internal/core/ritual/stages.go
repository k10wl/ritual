package ritual

// Stage names used in events and logs. Kept as plain strings so they
// survive JSON round-trip without type noise.
const (
	StageChecking   = "Checking"
	StagePulling    = "Pulling"
	StageAcquiring  = "Acquiring"
	StageRunning    = "Running"
	StageCommitting = "Committing"
	StagePushing    = "Pushing"
	StagePublishing = "Publishing"
	StageBackup     = "Backup"
	StageUnlocking  = "Unlocking"
	StageRetaining  = "Retaining"
	StageFailed     = "Failed"
	StageDone       = "Done"
)

// Named is implemented by every stage strategy. The ritual driver uses
// this to emit StateChangedInfo on each transition.
type Named interface {
	Name() string
}
