package selfupdate

// CheckRequested is the GUI command behind Advanced ▸ "Check for update"
// (design-log/037 §Q6). The control adapter publishes it; the composition
// root subscribes and runs the same Check→Apply flow as launch — one code
// path, one Preflight takeover. Carries no fields: the flow reads everything
// from the remote.
type CheckRequested struct{}

func (CheckRequested) String() string { return "update: check requested" }
