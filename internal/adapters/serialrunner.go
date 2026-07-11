package adapters

import (
	"context"
	"ritual/internal/core/ports"
)

// SerialRunner runs items in input order on the calling goroutine. Default
// BlobRunner used in tests and as the conservative composition-root choice.
// Stops at the first non-nil fn error or ctx cancellation. Weight is ignored —
// callers that need deterministic order rely on input order being preserved.
type SerialRunner struct{}

// NewSerialRunner returns a SerialRunner.
func NewSerialRunner() SerialRunner { return SerialRunner{} }

// Run processes items sequentially in input order, stopping at the first error.
func (SerialRunner) Run(ctx context.Context, items []ports.BlobItem, fn func(context.Context, string) error) error {
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(ctx, item.Key); err != nil {
			return err
		}
	}
	return nil
}

var _ ports.BlobRunner = SerialRunner{}
