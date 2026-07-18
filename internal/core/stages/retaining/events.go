package retaining

import "fmt"

// RetentionStartedInfo fires once per Retention Job, just before its Run
// is invoked. Label distinguishes side+keyspace (e.g. "refs-local",
// "logs-local", "refs-remote").
type RetentionStartedInfo struct{ Label string }

func (r RetentionStartedInfo) String() string { return "retention start " + r.Label }

// RetentionFinishedInfo fires after a Retention Job's Run returns. Err
// mirrors the Job's return; nil means success.
type RetentionFinishedInfo struct {
	Label string
	Err   error
}

func (r RetentionFinishedInfo) String() string {
	if r.Err != nil {
		return fmt.Sprintf("retention finish %s err=%v", r.Label, r.Err)
	}
	return "retention finish " + r.Label
}

// GCStartedInfo fires once per GC Job, just before its Run is invoked.
type GCStartedInfo struct{ Label string }

func (g GCStartedInfo) String() string { return "gc start " + g.Label }

// GCFinishedInfo fires after a GC Job's Run returns.
type GCFinishedInfo struct {
	Label string
	Err   error
}

func (g GCFinishedInfo) String() string {
	if g.Err != nil {
		return fmt.Sprintf("gc finish %s err=%v", g.Label, g.Err)
	}
	return "gc finish " + g.Label
}
