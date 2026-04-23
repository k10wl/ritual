package adapters

import "fmt"

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
