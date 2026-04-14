package services

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"ritual/internal/core/ports"
)

// CleanupDownloadPhase deletes local ghost files and removes sync staging folder.
// Failure here = ghost files linger until next session. Acceptable.
type CleanupDownloadPhase struct {
	local      ports.StorageRepository
	worldsRoot string
	xxhashMap  map[string]string
	syncPrefix string
}

func (p *CleanupDownloadPhase) Name() string { return "sync-download-cleanup" }

func (p *CleanupDownloadPhase) Execute(ctx context.Context) error {
	if err := deleteLocalGhosts(p.worldsRoot, p.xxhashMap); err != nil {
		return err
	}
	_ = p.local.Delete(ctx, p.syncPrefix)
	return nil
}

func (p *CleanupDownloadPhase) Verify(ctx context.Context) error {
	return nil // ghost cleanup — failure acceptable
}

// CleanupUploadPhase deletes orphaned remote files and removes sync staging folder.
// Failure here = ghost files on remote until next session. Acceptable.
type CleanupUploadPhase struct {
	remote     ports.StorageRepository
	files      []string
	syncPrefix string
	events     chan<- ports.Event
}

func (p *CleanupUploadPhase) Name() string { return "sync-upload-cleanup" }

func (p *CleanupUploadPhase) Execute(ctx context.Context) error {
	for _, filePath := range p.files {
		ports.SendEvent(p.events, ports.UpdateEvent{
			Operation: "sync-upload",
			Message:   "Deleting remote orphan",
			Data:      map[string]any{"file": filePath},
		})
		if err := p.remote.Delete(ctx, filepath.ToSlash(filepath.Join("worlds", filePath))); err != nil {
			return fmt.Errorf("delete %s: %w", filePath, err)
		}
	}

	// Clean sync staging folder (List+Delete for R2 flat storage)
	keys, err := p.remote.List(ctx, p.syncPrefix)
	if err == nil {
		for _, key := range keys {
			_ = p.remote.Delete(ctx, key)
		}
	}
	_ = p.remote.Delete(ctx, p.syncPrefix)

	return nil
}

func (p *CleanupUploadPhase) Verify(ctx context.Context) error {
	return nil // ghost cleanup — failure acceptable
}

// deleteLocalGhosts walks worlds directory and removes files not in manifest.
func deleteLocalGhosts(worldsRoot string, xxhashMap map[string]string) error {
	if worldsRoot == "" {
		return nil
	}
	return filepath.WalkDir(worldsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(worldsRoot, path)
		if relErr != nil {
			return relErr
		}
		if _, exists := xxhashMap[filepath.ToSlash(rel)]; !exists {
			return os.Remove(path)
		}
		return nil
	})
}
