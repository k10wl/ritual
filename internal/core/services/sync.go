package services

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	retry "github.com/avast/retry-go/v4"

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

// Sync retry configuration
const (
	syncMaxRetries  = 5
	syncBaseDelay   = 1 * time.Second
	syncMaxDelay    = 15 * time.Second
)

// SyncService orchestrates delta sync upload and download flows.
// Coordinates P1→P2→P3 phases for both directions using WorldScanner,
// StorageRepository, and LibrarianService.
type SyncService struct {
	scanner   ports.WorldScanner
	local     ports.StorageRepository
	remote    ports.StorageRepository
	librarian ports.LibrarianService
	events    chan<- ports.Event
}

// NewSyncService creates a new sync service with all dependencies injected.
func NewSyncService(
	scanner ports.WorldScanner,
	local ports.StorageRepository,
	remote ports.StorageRepository,
	librarian ports.LibrarianService,
	events chan<- ports.Event,
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
		scanner:   scanner,
		local:     local,
		remote:    remote,
		librarian: librarian,
		events:    events,
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

	// Fetch manifests
	localManifest, err := s.librarian.GetLocalManifest(ctx)
	if err != nil {
		return fmt.Errorf("get local manifest: %w", err)
	}
	remoteManifest, err := s.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return fmt.Errorf("get remote manifest: %w", err)
	}

	// String comparison only — zero IO
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

	for i, path := range diff.Download {
		filePath := path
		s.send(ports.UpdateEvent{Operation: "sync-download", Message: "Downloading file", Data: map[string]any{
			"file":     filePath,
			"progress": i + 1,
			"total":    len(diff.Download),
		}})

		err := retry.Do(func() error {
			data, getErr := s.remote.Get(ctx, filepath.ToSlash(filepath.Join("worlds", filePath)))
			if getErr != nil {
				return getErr
			}
			return s.local.Put(ctx, filepath.ToSlash(filepath.Join(syncPrefix, filePath)), data)
		},
			retry.Attempts(syncMaxRetries),
			retry.Delay(syncBaseDelay),
			retry.MaxDelay(syncMaxDelay),
			retry.DelayType(retry.BackOffDelay),
			retry.Context(ctx),
		)
		if err != nil {
			return fmt.Errorf("download P1 failed for %s: %w", filePath, err)
		}
	}
	s.send(ports.FinishEvent{Operation: "sync-download-p1"})

	// P2: Move from sync to worlds + update local manifest
	s.send(ports.StartEvent{Operation: "sync-download-p2"})
	for _, path := range diff.Download {
		filePath := path
		err := retry.Do(func() error {
			syncKey := filepath.ToSlash(filepath.Join(syncPrefix, filePath))
			data, getErr := s.local.Get(ctx, syncKey)
			if getErr != nil {
				return getErr
			}
			if putErr := s.local.Put(ctx, filepath.ToSlash(filepath.Join("worlds", filePath)), data); putErr != nil {
				return putErr
			}
			return s.local.Delete(ctx, syncKey)
		},
			retry.Attempts(syncMaxRetries),
			retry.Delay(syncBaseDelay),
			retry.MaxDelay(syncMaxDelay),
			retry.DelayType(retry.BackOffDelay),
			retry.Context(ctx),
		)
		if err != nil {
			return fmt.Errorf("download P2 failed for %s: %w", filePath, err)
		}
	}

	// Update local manifest to match remote
	localManifest.XXHashMap = remoteManifest.XXHashMap
	localManifest.XXHashSyncAt = remoteManifest.XXHashSyncAt
	if err := s.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return fmt.Errorf("save local manifest after download P2: %w", err)
	}
	s.send(ports.FinishEvent{Operation: "sync-download-p2"})

	// P3: Delete local ghost files
	s.send(ports.StartEvent{Operation: "sync-download-p3"})
	localFiles, err := s.local.List(ctx, "worlds")
	if err != nil {
		return fmt.Errorf("list local worlds for ghost cleanup: %w", err)
	}

	for _, key := range localFiles {
		rel := filepath.ToSlash(key)
		// Strip "worlds/" prefix for manifest lookup
		if len(rel) > 7 && rel[:7] == "worlds/" {
			rel = rel[7:]
		}
		if _, exists := localManifest.XXHashMap[rel]; !exists {
			s.send(ports.UpdateEvent{Operation: "sync-download", Message: "Deleting local ghost", Data: map[string]any{"file": rel}})
			if delErr := s.local.Delete(ctx, key); delErr != nil {
				return fmt.Errorf("delete local ghost %s: %w", key, delErr)
			}
		}
	}
	s.send(ports.FinishEvent{Operation: "sync-download-p3"})

	// Clean sync folder
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

	// Walk worlds, compute xxhash → new_map (disk is truth)
	newMap, err := s.scanner.Scan(ctx)
	if err != nil {
		return fmt.Errorf("scan worlds: %w", err)
	}

	// Overwrite local manifest with new map
	localManifest, err := s.librarian.GetLocalManifest(ctx)
	if err != nil {
		return fmt.Errorf("get local manifest: %w", err)
	}
	localManifest.XXHashMap = newMap
	localManifest.XXHashSyncAt = time.Now()
	if err := s.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return fmt.Errorf("save local manifest after scan: %w", err)
	}

	// Diff against remote
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

		for i, path := range diff.Upload {
			filePath := path
			s.send(ports.UpdateEvent{Operation: "sync-upload", Message: "Uploading file", Data: map[string]any{
				"file":     filePath,
				"progress": i + 1,
				"total":    len(diff.Upload),
			}})

			err := retry.Do(func() error {
				data, getErr := s.local.Get(ctx, filepath.ToSlash(filepath.Join("worlds", filePath)))
				if getErr != nil {
					return getErr
				}
				return s.remote.Put(ctx, filepath.ToSlash(filepath.Join(syncPrefix, filePath)), data)
			},
				retry.Attempts(syncMaxRetries),
				retry.Delay(syncBaseDelay),
				retry.MaxDelay(syncMaxDelay),
				retry.DelayType(retry.BackOffDelay),
				retry.Context(ctx),
			)
			if err != nil {
				return fmt.Errorf("upload P1 failed for %s: %w", filePath, err)
			}
		}
		s.send(ports.FinishEvent{Operation: "sync-upload-p1"})

		// P2: Move from remote sync to remote worlds + update remote manifest
		s.send(ports.StartEvent{Operation: "sync-upload-p2"})
		for _, path := range diff.Upload {
			filePath := path
			err := retry.Do(func() error {
				src := filepath.ToSlash(filepath.Join(syncPrefix, filePath))
				dst := filepath.ToSlash(filepath.Join("worlds", filePath))
				if copyErr := s.remote.Copy(ctx, src, dst); copyErr != nil {
					return copyErr
				}
				return s.remote.Delete(ctx, src)
			},
				retry.Attempts(syncMaxRetries),
				retry.Delay(syncBaseDelay),
				retry.MaxDelay(syncMaxDelay),
				retry.DelayType(retry.BackOffDelay),
				retry.Context(ctx),
			)
			if err != nil {
				return fmt.Errorf("upload P2 failed for %s: %w", filePath, err)
			}
		}

		remoteManifest.XXHashMap = newMap
		remoteManifest.XXHashSyncAt = localManifest.XXHashSyncAt
		if err := s.librarian.SaveRemoteManifest(ctx, remoteManifest); err != nil {
			return fmt.Errorf("save remote manifest after upload P2: %w", err)
		}
		s.send(ports.FinishEvent{Operation: "sync-upload-p2"})
	}

	// P3: Delete orphaned files from remote worlds
	if len(diff.Delete) > 0 {
		s.send(ports.StartEvent{Operation: "sync-upload-p3"})
		for _, path := range diff.Delete {
			filePath := path
			s.send(ports.UpdateEvent{Operation: "sync-upload", Message: "Deleting remote orphan", Data: map[string]any{"file": filePath}})

			err := retry.Do(func() error {
				return s.remote.Delete(ctx, filepath.ToSlash(filepath.Join("worlds", filePath)))
			},
				retry.Attempts(syncMaxRetries),
				retry.Delay(syncBaseDelay),
				retry.MaxDelay(syncMaxDelay),
				retry.DelayType(retry.BackOffDelay),
				retry.Context(ctx),
			)
			if err != nil {
				return fmt.Errorf("upload P3 failed for %s: %w", filePath, err)
			}
		}
		s.send(ports.FinishEvent{Operation: "sync-upload-p3"})
	}

	// Clean remote sync folder
	s.cleanRemoteSyncFolder(ctx, syncPrefix)

	s.send(ports.FinishEvent{Operation: "sync-upload"})
	return nil
}

// cleanSyncFolder removes local sync staging directory contents
func (s *SyncService) cleanSyncFolder(ctx context.Context, prefix string) {
	keys, err := s.local.List(ctx, prefix)
	if err != nil {
		return
	}
	for _, key := range keys {
		_ = s.local.Delete(ctx, key)
	}
}

// cleanRemoteSyncFolder removes remote sync staging directory contents
func (s *SyncService) cleanRemoteSyncFolder(ctx context.Context, prefix string) {
	keys, err := s.remote.List(ctx, prefix)
	if err != nil {
		return
	}
	for _, key := range keys {
		_ = s.remote.Delete(ctx, key)
	}
}
