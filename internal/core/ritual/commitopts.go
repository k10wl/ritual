package ritual

import (
	"ritual/internal/core/ports"
)

// NewCommitOptsResolver builds the composition-root resolver for the
// committing stage. Returned func is assignable to committing.OptsResolver.
// Policy (spec §1435, committing doc §amend-push collapse):
//   - rs.RefID != ""  → Amend=rs.RefID. A live-ticker draft exists for
//     this session; the post-session commit collapses into it.
//   - rs.RefID == ""  → fresh commit parented on rs.ParentRefID (the
//     pulled HEAD). No ticker ever ran, so no draft exists.
func NewCommitOptsResolver(targets []string) func(*RunState) ports.CommitOpts {
	return func(rs *RunState) ports.CommitOpts {
		if rs.RefID != "" {
			return ports.CommitOpts{Amend: rs.RefID, Targets: targets}
		}
		return ports.CommitOpts{Parent: rs.ParentRefID, Targets: targets}
	}
}
