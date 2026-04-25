package ritual_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

func TestNewCommitOptsResolver_FreshCommitUsesParentRefIDAsParent(t *testing.T) {
	resolver := ritual.NewCommitOptsResolver([]string{"world/**"})
	rs := &ritual.RunState{ParentRefID: "pulled-head"}

	got := resolver(rs)

	assert.Equal(t, ports.CommitOpts{
		Parent:  domain.RefID("pulled-head"),
		Targets: []string{"world/**"},
	}, got,
		"CommitOptsResolver with empty rs.RefID must build a fresh-commit opts set with Parent=rs.ParentRefID — the pulled HEAD becomes the new ref's predecessor so history is linear")
}

func TestNewCommitOptsResolver_AmendCollapsesLiveTickerDraft(t *testing.T) {
	resolver := ritual.NewCommitOptsResolver([]string{"world/**"})
	rs := &ritual.RunState{RefID: "draft-from-ticker", ParentRefID: "pulled-head"}

	got := resolver(rs)

	assert.Equal(t, ports.CommitOpts{
		Amend:   domain.RefID("draft-from-ticker"),
		Targets: []string{"world/**"},
	}, got,
		"CommitOptsResolver with non-empty rs.RefID must build an amend opts set (Amend=rs.RefID, Parent unset) so the post-session commit collapses into the live-ticker draft instead of forking a sibling ref (spec §1435)")
}
