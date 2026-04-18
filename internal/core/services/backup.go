package services

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"time"
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

// CreateBackupFrom copies files listed in the manifest's XXHashMap from
// src storage to dst storage under dstPrefix/{ts}/srcPrefix/... . Uses
// the manifest as the authoritative file list rather than walking the
// source tree, avoiding FSRepository.List's single-level behaviour.
func CreateBackupFrom(
	ctx context.Context,
	src, dst ports.StorageRepository,
	srcPrefix, dstPrefix string,
	manifest *domain.Manifest,
) error {
	ts := time.Now().UTC().Format(config.TimestampFormat)
	base := path.Join(dstPrefix, ts)

	for relPath := range manifest.Worlds.XXHashMap {
		key := srcPrefix + "/" + relPath
		data, err := src.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("get %s: %w", key, err)
		}
		if err := dst.Put(ctx, path.Join(base, key), data); err != nil {
			return fmt.Errorf("put %s: %w", key, err)
		}
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := dst.Put(ctx, path.Join(base, "manifest.json"), data); err != nil {
		return fmt.Errorf("put manifest: %w", err)
	}

	return nil
}
