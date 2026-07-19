// Package retention is the retention engine and its keyspace-specific
// concretes. Retention is pure policy: given keys, select which to drop.
// Deletion is caller-side. See §Retention and GC in the v2.1 design.
package retention

import "context"

// Retention decides which storage keys fall outside the rules.
// Select is pure of side effects: it lists, classifies, returns.
type Retention interface {
	Select(ctx context.Context) ([]string, error)
}
