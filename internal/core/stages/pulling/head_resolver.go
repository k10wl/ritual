package pulling

import (
	"context"
	"fmt"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strings"
)

// NewHeadResolver returns the lexicographically greatest refs/{id}.json key
// (timestamps sort as strings) from the given storage. An empty refs/ listing
// surfaces as ErrNoHead so callers can distinguish "fresh storage, advance to
// bootstrap" from a real listing failure that must route to onFail.
func NewHeadResolver(storage ports.StorageRepository) HeadResolver {
	return func(ctx context.Context) (domain.RefID, error) {
		keys, err := storage.List(ctx, "refs/")
		if err != nil {
			return "", fmt.Errorf("list refs: %w", err)
		}
		var head string
		for _, key := range keys {
			name := strings.TrimSuffix(strings.TrimPrefix(key, "refs/"), ".json")
			if name == "" {
				continue
			}
			if name > head {
				head = name
			}
		}
		if head == "" {
			return "", ErrNoHead
		}
		return domain.RefID(head), nil
	}
}
