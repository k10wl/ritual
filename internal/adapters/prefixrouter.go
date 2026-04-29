package adapters

import (
	"context"
	"fmt"
	"io"
	"ritual/internal/core/ports"
	"strings"
)

// PrefixRouter splits a single StorageRepository facade across two backing
// stores by key prefix: keys with Prefix go to Routed, everything else to
// Fallback. Used to gate a per-keyspace decorator (e.g. compression on
// objects/) while keeping other keyspaces (refs/, lock, settings) on a raw
// store so their bytes stay human-readable on disk.
//
// Cross-gate Copy falls back to GetStream → PutStream so the source's read
// codec and the destination's write codec stay isolated.
type PrefixRouter struct {
	prefix   string
	routed   ports.StorageRepository
	fallback ports.StorageRepository
}

// NewPrefixRouter routes keys under prefix to routed, all others to fallback.
func NewPrefixRouter(prefix string, routed, fallback ports.StorageRepository) *PrefixRouter {
	return &PrefixRouter{prefix: prefix, routed: routed, fallback: fallback}
}

func (r *PrefixRouter) String() string {
	return fmt.Sprintf("router{%s→%s, *→%s}", r.prefix, r.routed, r.fallback)
}

func (r *PrefixRouter) pick(key string) ports.StorageRepository {
	if strings.HasPrefix(key, r.prefix) {
		return r.routed
	}
	return r.fallback
}

func (r *PrefixRouter) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	return r.pick(key).GetStream(ctx, key)
}

func (r *PrefixRouter) PutStream(ctx context.Context, key string, body io.Reader) error {
	return r.pick(key).PutStream(ctx, key, body)
}

func (r *PrefixRouter) Exists(ctx context.Context, key string) (bool, error) {
	return r.pick(key).Exists(ctx, key)
}

func (r *PrefixRouter) Delete(ctx context.Context, key string) error {
	return r.pick(key).Delete(ctx, key)
}

func (r *PrefixRouter) DeleteBatch(ctx context.Context, keys []string) error {
	routed, fallback := splitByPrefix(keys, r.prefix)
	if len(routed) > 0 {
		if err := r.routed.DeleteBatch(ctx, routed); err != nil {
			return err
		}
	}
	if len(fallback) > 0 {
		if err := r.fallback.DeleteBatch(ctx, fallback); err != nil {
			return err
		}
	}
	return nil
}

func (r *PrefixRouter) List(ctx context.Context, prefix string) ([]string, error) {
	if strings.HasPrefix(prefix, r.prefix) {
		return r.routed.List(ctx, prefix)
	}
	if strings.HasPrefix(r.prefix, prefix) {
		routedKeys, err := r.routed.List(ctx, prefix)
		if err != nil {
			return nil, err
		}
		fallbackKeys, err := r.fallback.List(ctx, prefix)
		if err != nil {
			return nil, err
		}
		return append(fallbackKeys, routedKeys...), nil
	}
	return r.fallback.List(ctx, prefix)
}

func (r *PrefixRouter) Copy(ctx context.Context, sourceKey, destKey string) error {
	src := r.pick(sourceKey)
	dst := r.pick(destKey)
	if src == dst {
		return src.Copy(ctx, sourceKey, destKey)
	}
	body, err := src.GetStream(ctx, sourceKey)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	return dst.PutStream(ctx, destKey, body)
}

func splitByPrefix(keys []string, prefix string) (matching, other []string) {
	for _, k := range keys {
		if strings.HasPrefix(k, prefix) {
			matching = append(matching, k)
		} else {
			other = append(other, k)
		}
	}
	return
}

var _ ports.StorageRepository = (*PrefixRouter)(nil)
