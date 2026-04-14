package services

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// CreateBackup copies all keys under srcPrefix into dstPrefix/{ts}/... within the same storage,
// then writes a manifest snapshot at dstPrefix/{ts}/manifest.json.
// Same-storage copy is efficient: same-disk copy locally, server-side copy on R2.
func CreateBackup(
	ctx context.Context,
	storage ports.StorageRepository,
	srcPrefix, dstPrefix string,
	manifest *domain.Manifest,
) error {
	ts := time.Now().UTC().Format(config.TimestampFormat)
	base := path.Join(dstPrefix, ts)

	keys, err := storage.List(ctx, srcPrefix)
	if err != nil {
		return fmt.Errorf("list %s: %w", srcPrefix, err)
	}

	for _, key := range keys {
		if err := storage.Copy(ctx, key, path.Join(base, key)); err != nil {
			return fmt.Errorf("copy %s: %w", key, err)
		}
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := storage.Put(ctx, path.Join(base, "manifest.json"), data); err != nil {
		return fmt.Errorf("put manifest: %w", err)
	}

	return nil
}
