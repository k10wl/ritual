// Package machine is a generic state-machine driver. It imports nothing
// from the ritual domain — stages are plain strategies keyed by typed
// successor fields, and the driver loops until a strategy returns nil.
//
// The driver never inspects state semantics; it only advances. Transition
// topology lives in the composition root as constructor wiring, not in
// this package.
package machine

import "context"

// Strategy is one stage of work. Run performs the stage and returns the
// next Strategy to execute, or nil to terminate. The state argument S is
// a per-run value shared across all stages (e.g. ritual.RunState).
type Strategy[S any] interface {
	Run(ctx context.Context, s *S) (Strategy[S], error)
}

// Drive advances strategies starting at start until one returns nil.
// An error from any Run aborts the loop and propagates.
func Drive[S any](ctx context.Context, s *S, start Strategy[S]) error {
	cur := start
	for cur != nil {
		next, err := cur.Run(ctx, s)
		if err != nil {
			return err
		}
		cur = next
	}
	return nil
}
