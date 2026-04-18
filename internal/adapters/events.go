package adapters

import "fmt"

// RetryAttemptInfo is published by the R2 adapter on each retry attempt.
// Key is the object key (or empty for ops that don't target a single key, e.g. List).
// Attempt is 1-indexed; Operation is adapter.method (e.g. "r2.Get", "r2.DeleteBatch").
type RetryAttemptInfo struct {
	Operation string
	Key       string
	Attempt   uint
	Err       error
}

func (r RetryAttemptInfo) String() string {
	if r.Key == "" {
		return fmt.Sprintf("retry %s attempt=%d err=%v", r.Operation, r.Attempt, r.Err)
	}
	return fmt.Sprintf("retry %s key=%s attempt=%d err=%v", r.Operation, r.Key, r.Attempt, r.Err)
}

// ReadinessDialInfo fires on every TCP readiness probe attempt. Attempt is
// 1-indexed. Err is nil on the dial that succeeds (the final one); non-nil
// on every prior failed dial. Address is what the probe actually dialed.
type ReadinessDialInfo struct {
	Address string
	Attempt uint
	Err     error
}

func (r ReadinessDialInfo) String() string {
	if r.Err == nil {
		return fmt.Sprintf("readiness dial %s attempt=%d ok", r.Address, r.Attempt)
	}
	return fmt.Sprintf("readiness dial %s attempt=%d err=%v", r.Address, r.Attempt, r.Err)
}
