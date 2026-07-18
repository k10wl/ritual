package livesync

import (
	"fmt"
	"ritual/internal/core/domain"
)

// LiveDraftCommitted fires after a tick's Commit succeeds. RefID is the
// id the next tick must Amend (and the post-session committing.Strategy
// must Amend on graceful exit). Publishing is decoupled from Push
// outcome — see design-log/016 §"amend gap" — so subscribers must treat
// this as "local draft on disk", not "remote synced".
//
// Composition root subscribes a tiny dispatcher that writes rs.RefID
// from this event; livesync stays unaware of RunState (OQ4 Option A).
type LiveDraftCommitted struct {
	RefID domain.RefID
}

func (e LiveDraftCommitted) String() string {
	return fmt.Sprintf("live draft committed: %s", e.RefID)
}
