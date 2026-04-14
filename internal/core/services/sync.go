package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// Sync error constants
var (
	ErrSyncScannerNil   = errors.New("world scanner cannot be nil")
	ErrSyncLocalNil     = errors.New("local storage cannot be nil")
	ErrSyncRemoteNil    = errors.New("remote storage cannot be nil")
	ErrSyncLibrarianNil = errors.New("librarian service cannot be nil")
	ErrSyncNil          = errors.New("sync service cannot be nil")
)

// SyncService orchestrates delta sync upload and download flows.
// Coordinates P1→P2→P3 phases for both directions using WorldScanner,
// StorageRepository, and LibrarianService.
// Retry logic belongs in the StorageRepository decorator, not here.
type SyncService struct {
	scanner    ports.WorldScanner
	local      ports.StorageRepository
	remote     ports.StorageRepository
	librarian  ports.LibrarianService
	events     chan<- ports.Event
	worldsRoot string // absolute path to local worlds directory, used for ghost cleanup walk
}

// NewSyncService creates a new sync service with all dependencies injected.
func NewSyncService(
	scanner ports.WorldScanner,
	local ports.StorageRepository,
	remote ports.StorageRepository,
	librarian ports.LibrarianService,
	events chan<- ports.Event,
	worldsRoot string,
) (*SyncService, error) {
	if scanner == nil {
		return nil, ErrSyncScannerNil
	}
	if local == nil {
		return nil, ErrSyncLocalNil
	}
	if remote == nil {
		return nil, ErrSyncRemoteNil
	}
	if librarian == nil {
		return nil, ErrSyncLibrarianNil
	}
	return &SyncService{
		scanner:    scanner,
		local:      local,
		remote:     remote,
		librarian:  librarian,
		events:     events,
		worldsRoot: worldsRoot,
	}, nil
}

// send safely sends an event to the channel
func (s *SyncService) send(evt ports.Event) {
	ports.SendEvent(s.events, evt)
}

// Download executes the download flow: fetch remote manifest, diff against local,
// download changed files to sync staging, move to worlds, delete local ghosts.
func (s *SyncService) Download(ctx context.Context) error {
	if s == nil {
		return ErrSyncNil
	}

	s.send(ports.StartEvent{Operation: "sync-download"})

	localManifest, err := s.librarian.GetLocalManifest(ctx)
	if err != nil {
		return fmt.Errorf("get local manifest: %w", err)
	}
	remoteManifest, err := s.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return fmt.Errorf("get remote manifest: %w", err)
	}

	diff := domain.ComputeDiff(localManifest.XXHashMap, remoteManifest.XXHashMap)

	if len(diff.Download) == 0 {
		s.send(ports.FinishEvent{Operation: "sync-download"})
		return nil
	}

	syncPrefix := fmt.Sprintf("sync/%d", time.Now().UnixNano())

	// P1: Download changed files to local sync staging
	s.send(ports.StartEvent{Operation: "sync-download-p1"})
	s.send(ports.UpdateEvent{Operation: "sync-download", Message: "Starting download phase", Data: map[string]any{
		"total_files": len(diff.Download),
	}})

	for i, filePath := range diff.Download {
		s.send(ports.UpdateEvent{Operation: "sync-download", Message: "Downloading file", Data: map[string]any{
			"file": filePath, "progress": i + 1, "total": len(diff.Download),
		}})

		data, getErr := s.remote.Get(ctx, filepath.ToSlash(filepath.Join("worlds", filePath)))
		if getErr != nil {
			return fmt.Errorf("download P1 failed for %s: %w", filePath, getErr)
		}
		if putErr := s.local.Put(ctx, filepath.ToSlash(filepath.Join(syncPrefix, filePath)), data); putErr != nil {
			return fmt.Errorf("download P1 staging failed for %s: %w", filePath, putErr)
		}
	}
	s.send(ports.FinishEvent{Operation: "sync-download-p1"})

	// P2: Move from sync to worlds + update local manifest
	s.send(ports.StartEvent{Operation: "sync-download-p2"})
	for _, filePath := range diff.Download {
		syncKey := filepath.ToSlash(filepath.Join(syncPrefix, filePath))
		data, getErr := s.local.Get(ctx, syncKey)
		if getErr != nil {
			return fmt.Errorf("download P2 read failed for %s: %w", filePath, getErr)
		}
		if putErr := s.local.Put(ctx, filepath.ToSlash(filepath.Join("worlds", filePath)), data); putErr != nil {
			return fmt.Errorf("download P2 write failed for %s: %w", filePath, putErr)
		}
		if delErr := s.local.Delete(ctx, syncKey); delErr != nil {
			return fmt.Errorf("download P2 cleanup failed for %s: %w", filePath, delErr)
		}
	}

	localManifest.XXHashMap = remoteManifest.XXHashMap
	localManifest.XXHashSyncAt = remoteManifest.XXHashSyncAt
	if err := s.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return fmt.Errorf("save local manifest after download P2: %w", err)
	}
	s.send(ports.FinishEvent{Operation: "sync-download-p2"})

	// P3: Delete local ghost files — walk worlds dir, delete anything not in manifest
	s.send(ports.StartEvent{Operation: "sync-download-p3"})
	if ghostErr := s.deleteLocalGhosts(ctx, localManifest.XXHashMap); ghostErr != nil {
		return fmt.Errorf("download P3 ghost cleanup: %w", ghostErr)
	}
	s.send(ports.FinishEvent{Operation: "sync-download-p3"})

	s.cleanSyncFolder(ctx, syncPrefix)
	s.send(ports.FinishEvent{Operation: "sync-download"})
	return nil
}

// Upload executes the upload flow: scan worlds, diff against remote manifest,
// upload changed files to sync staging, move to remote worlds, delete orphaned remote files.
func (s *SyncService) Upload(ctx context.Context) error {
	if s == nil {
		return ErrSyncNil
	}

	s.send(ports.StartEvent{Operation: "sync-upload"})

	newMap, err := s.scanner.Scan(ctx)
	if err != nil {
		return fmt.Errorf("scan worlds: %w", err)
	}

	localManifest, err := s.librarian.GetLocalManifest(ctx)
	if err != nil {
		return fmt.Errorf("get local manifest: %w", err)
	}
	localManifest.XXHashMap = newMap
	localManifest.XXHashSyncAt = time.Now()
	if err := s.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return fmt.Errorf("save local manifest after scan: %w", err)
	}

	remoteManifest, err := s.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return fmt.Errorf("get remote manifest: %w", err)
	}

	diff := domain.ComputeDiff(newMap, remoteManifest.XXHashMap)

	if len(diff.Upload) == 0 && len(diff.Delete) == 0 {
		s.send(ports.FinishEvent{Operation: "sync-upload"})
		return nil
	}

	syncPrefix := fmt.Sprintf("sync/%d", time.Now().UnixNano())

	// P1: Upload changed files to remote sync staging
	if len(diff.Upload) > 0 {
		s.send(ports.StartEvent{Operation: "sync-upload-p1"})
		s.send(ports.UpdateEvent{Operation: "sync-upload", Message: "Starting upload phase", Data: map[string]any{
			"total_files": len(diff.Upload),
		}})

		for i, filePath := range diff.Upload {
			s.send(ports.UpdateEvent{Operation: "sync-upload", Message: "Uploading file", Data: map[string]any{
				"file": filePath, "progress": i + 1, "total": len(diff.Upload),
			}})

			data, getErr := s.local.Get(ctx, filepath.ToSlash(filepath.Join("worlds", filePath)))
			if getErr != nil {
				return fmt.Errorf("upload P1 failed for %s: %w", filePath, getErr)
			}
			if putErr := s.remote.Put(ctx, filepath.ToSlash(filepath.Join(syncPrefix, filePath)), data); putErr != nil {
				return fmt.Errorf("upload P1 staging failed for %s: %w", filePath, putErr)
			}
		}
		s.send(ports.FinishEvent{Operation: "sync-upload-p1"})

		// P2: Move from remote sync to remote worlds + update remote manifest
		s.send(ports.StartEvent{Operation: "sync-upload-p2"})
		for _, filePath := range diff.Upload {
			src := filepath.ToSlash(filepath.Join(syncPrefix, filePath))
			dst := filepath.ToSlash(filepath.Join("worlds", filePath))
			if copyErr := s.remote.Copy(ctx, src, dst); copyErr != nil {
				return fmt.Errorf("upload P2 copy failed for %s: %w", filePath, copyErr)
			}
			if delErr := s.remote.Delete(ctx, src); delErr != nil {
				return fmt.Errorf("upload P2 cleanup failed for %s: %w", filePath, delErr)
			}
		}

		s.send(ports.FinishEvent{Operation: "sync-upload-p2"})
	}

	// Update remote manifest with new map (covers both upload and delete-only cases)
	remoteManifest.XXHashMap = newMap
	remoteManifest.XXHashSyncAt = localManifest.XXHashSyncAt
	if err := s.librarian.SaveRemoteManifest(ctx, remoteManifest); err != nil {
		return fmt.Errorf("save remote manifest: %w", err)
	}

	// P3: Delete orphaned files from remote worlds
	if len(diff.Delete) > 0 {
		s.send(ports.StartEvent{Operation: "sync-upload-p3"})
		for _, filePath := range diff.Delete {
			s.send(ports.UpdateEvent{Operation: "sync-upload", Message: "Deleting remote orphan", Data: map[string]any{"file": filePath}})
			if delErr := s.remote.Delete(ctx, filepath.ToSlash(filepath.Join("worlds", filePath))); delErr != nil {
				return fmt.Errorf("upload P3 failed for %s: %w", filePath, delErr)
			}
		}
		s.send(ports.FinishEvent{Operation: "sync-upload-p3"})
	}

	s.cleanRemoteSyncFolder(ctx, syncPrefix)
	s.send(ports.FinishEvent{Operation: "sync-upload"})
	return nil
}

// deleteLocalGhosts walks local worlds directory and deletes files not in manifest
func (s *SyncService) deleteLocalGhosts(ctx context.Context, xxhashMap map[string]string) error {
	if s.worldsRoot == "" {
		return nil
	}
	return filepath.WalkDir(s.worldsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(s.worldsRoot, path)
		if relErr != nil {
			return relErr
		}
		key := filepath.ToSlash(rel)

		if _, exists := xxhashMap[key]; !exists {
			s.send(ports.UpdateEvent{Operation: "sync-download", Message: "Deleting local ghost", Data: map[string]any{"file": key}})
			return os.Remove(path)
		}
		return nil
	})
}

// cleanSyncFolder removes local sync staging directory
func (s *SyncService) cleanSyncFolder(ctx context.Context, prefix string) {
	_ = s.local.Delete(ctx, prefix)
}

// cleanRemoteSyncFolder removes remote sync staging directory
func (s *SyncService) cleanRemoteSyncFolder(ctx context.Context, prefix string) {
	_ = s.remote.Delete(ctx, prefix)
}
