package acquiring

import "fmt"

// LockHeldInfo fires when the remote manifest's lease is still active and
// the run must abort. Holder is the current LockedBy value. Consumers
// (e.g. the GUI projection) surface the holder so the user knows who is
// playing.
type LockHeldInfo struct{ Holder string }

func (l LockHeldInfo) String() string { return fmt.Sprintf("lock held by %s", l.Holder) }
