package observed

import (
	"context"
	"fmt"
	"ritual/internal/core/ports"
	"ritual/internal/core/retention"
	"time"
)

// RetentionSelectInfo is published by the retention decorator once per
// Select call. Keys is the list returned from Select (possibly empty). Err
// mirrors the call's error; it does not alter control flow.
type RetentionSelectInfo struct {
	Label      string
	Keys       []string
	Count      int
	DurationMs int64
	Err        error
}

// String renders RetentionSelectInfo for log sinks. Keys are included
// verbatim so the record is self-contained.
func (r RetentionSelectInfo) String() string {
	if r.Err != nil {
		return fmt.Sprintf("retention.select label=%s err=%v dur=%dms", r.Label, r.Err, r.DurationMs)
	}
	return fmt.Sprintf("retention.select label=%s count=%d keys=%v dur=%dms", r.Label, r.Count, r.Keys, r.DurationMs)
}

type retentionDecorator struct {
	inner retention.Retention
	bus   ports.EventBus
	label string
}

// NewRetention decorates inner with bus-backed event publishing. The label
// distinguishes keyspace+side in the event stream (e.g. "refs-remote").
func NewRetention(inner retention.Retention, bus ports.EventBus, label string) retention.Retention {
	return &retentionDecorator{inner: inner, bus: bus, label: label}
}

// Select forwards to inner, publishes the event, and returns the inner result.
func (o *retentionDecorator) Select(ctx context.Context) ([]string, error) {
	start := time.Now()
	keys, err := o.inner.Select(ctx)
	if o.bus != nil {
		o.bus.Publish(RetentionSelectInfo{
			Label:      o.label,
			Keys:       keys,
			Count:      len(keys),
			DurationMs: time.Since(start).Milliseconds(),
			Err:        err,
		})
	}
	return keys, err
}
