package preprundup

import "time"

// HistoryLoader is the read side Estimator depends on — satisfied by *Store.
// A narrow interface keeps the estimator unit-testable against a fake.
type HistoryLoader interface {
	Load() (File, error)
}

// Estimator derives prep/wrap ETAs from the single most recently completed
// FlowSession run (design-log/058 "just store last one" deviation,
// 2026-08-28) — no averaging, no trimming, no window: each run's estimate is
// exactly the immediately preceding run's actual timing.
type Estimator struct {
	loader HistoryLoader
}

// NewEstimator wraps loader.
func NewEstimator(loader HistoryLoader) *Estimator {
	return &Estimator{loader: loader}
}

// PrepEta returns the last recorded prep duration, or 0 if no session has
// completed yet on this machine.
func (e *Estimator) PrepEta() time.Duration {
	return e.lastRun(func(s Sample) int64 { return s.PrepMs })
}

// WrapEta mirrors PrepEta for the wrap beat.
func (e *Estimator) WrapEta() time.Duration {
	return e.lastRun(func(s Sample) int64 { return s.WrapMs })
}

func (e *Estimator) lastRun(field func(Sample) int64) time.Duration {
	f, err := e.loader.Load()
	if err != nil || f.Last == nil {
		return 0
	}
	return time.Duration(field(*f.Last)) * time.Millisecond
}
