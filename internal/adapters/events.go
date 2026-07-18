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

// StorageRetryInfo is published by RetryingStorage before each retry attempt.
// Offset is non-zero only for mid-stream resume on GetStream; zero for
// call-level verbs (Exists, List, PutStream, ...). The store label points at
// the inner adapter, so a "retry::r2::bucket" row in the log is pinned to
// the actual layer that's flaky.
type StorageRetryInfo struct {
	Store       string
	Key         string
	Attempt     int
	MaxAttempts int
	Offset      int64
	Err         error
}

func (s StorageRetryInfo) String() string {
	if s.Offset > 0 {
		return fmt.Sprintf("storage.retry store=%s key=%s attempt=%d/%d offset=%d err=%v", s.Store, s.Key, s.Attempt, s.MaxAttempts, s.Offset, s.Err)
	}
	return fmt.Sprintf("storage.retry store=%s key=%s attempt=%d/%d err=%v", s.Store, s.Key, s.Attempt, s.MaxAttempts, s.Err)
}
