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
	StageUnlocking  = "Unlocking"
	StageRetaining  = "Retaining"
	// StageRetainingLocal / StageRetainingRemote attribute a retaining
	// failure to the side that actually failed so retry re-enters the
	// correct instance instead of replaying the local sweep before the
	// remote one. The strategy itself still reports StageRetaining; only
	// the failed-stage instance carries the suffix.
	StageRetainingLocal  = "Retaining/Local"
	StageRetainingRemote = "Retaining/Remote"
	StageFailed          = "Failed"
	StageDone            = "Done"
)

// Named is implemented by every stage strategy. The ritual driver uses
// this to emit StateChangedInfo on each transition.
type Named interface {
	Name() string
}
