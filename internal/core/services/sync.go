package services

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// SyncConfig holds the identity of a sync target.
type SyncConfig struct {
	Prefix   string // "worlds" or "server"
	LocalDir string // absolute path to final destination
}

type syncService struct {
	scanner       ports.DirectoryScanner
	local         ports.StorageRepository
	remote        ports.StorageRepository
	events        ports.EventBus
	config        SyncConfig
	localStaging  string
	remoteStaging string
}

var _ ports.SyncService = (*syncService)(nil)

func NewSyncService(
	scanner ports.DirectoryScanner,
	local, remote ports.StorageRepository,
	events ports.EventBus,
	config SyncConfig,
	localStaging string,
	remoteStaging string,
) *syncService {
	return &syncService{
		scanner:       scanner,
		local:         local,
		remote:        remote,
		events:        events,
		config:        config,
		localStaging:  localStaging,
		remoteStaging: remoteStaging,
	}
}

func (s *syncService) Download(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error) {
	// Clean leftover staging from previous sessions
	os.RemoveAll(s.localStaging)

	diff := domain.ComputeDiff(local.XXHashMap, remote.XXHashMap)
	if len(diff.Download) == 0 {
		return local, nil
	}

	defer os.RemoveAll(s.localStaging)

	s.send(ports.StartInfo{Operation: "sync-" + s.config.Prefix})

	if err := s.stageDownload(ctx, diff.Download); err != nil {
		return local, fmt.Errorf("stage: %w", err)
	}
	if err := s.commitDownload(); err != nil {
		return local, fmt.Errorf("commit: %w", err)
	}
	s.cleanLocalGhosts(remote.XXHashMap)

	s.send(ports.FinishInfo{Operation: "sync-" + s.config.Prefix})

	return domain.SyncState{
		XXHashMap:    remote.XXHashMap,
		XXHashSyncAt: remote.XXHashSyncAt,
	}, nil
}

func (s *syncService) Upload(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error) {
	// Clean leftover remote staging from previous sessions
	s.cleanRemoteStaging(ctx)

	newMap, err := s.scanner.Scan(ctx)
	if err != nil {
		return local, fmt.Errorf("scan: %w", err)
	}
	now := time.Now()

	diff := domain.ComputeDiff(newMap, remote.XXHashMap)
	if len(diff.Upload) == 0 && len(diff.Delete) == 0 {
		return domain.SyncState{XXHashMap: newMap, XXHashSyncAt: now}, nil
	}

	s.send(ports.StartInfo{Operation: "sync-" + s.config.Prefix})

	if len(diff.Upload) > 0 {
		if err := s.stageUpload(ctx, diff.Upload); err != nil {
			return local, fmt.Errorf("stage: %w", err)
		}
		if err := s.commitUpload(ctx, diff.Upload); err != nil {
			return local, fmt.Errorf("commit: %w", err)
		}
	}
	if len(diff.Delete) > 0 {
		s.cleanRemoteOrphans(ctx, diff.Delete)
	}
	s.cleanRemoteStaging(ctx)

	s.send(ports.FinishInfo{Operation: "sync-" + s.config.Prefix})

	return domain.SyncState{XXHashMap: newMap, XXHashSyncAt: now}, nil
}

func (s *syncService) send(evt ports.Event) {
	if s.events != nil { s.events.Publish(evt) }
}

// stageDownload downloads files from remote to local staging dir.
func (s *syncService) stageDownload(ctx context.Context, files []string) error {
	for i, file := range files {
		s.send(ports.UpdateInfo{
			Operation: "sync-" + s.config.Prefix,
			Message:   "Downloading",
			Data:      map[string]any{"file": file, "progress": i + 1, "total": len(files)},
		})
		srcKey := s.config.Prefix + "/" + file
		data, err := s.remote.Get(ctx, srcKey)
		if err != nil {
			return fmt.Errorf("get %s: %w", file, err)
		}
		dstPath := filepath.Join(s.localStaging, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", file, err)
		}
		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return fmt.Errorf("write staging %s: %w", file, err)
		}
	}
	return nil
}

// commitDownload walks staging dir and writes files to local target dir.
func (s *syncService) commitDownload() error {
	return fs.WalkDir(os.DirFS(s.localStaging), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == "." {
			return err
		}
		data, readErr := os.ReadFile(filepath.Join(s.localStaging, path))
		if readErr != nil {
			return readErr
		}
		dstPath := filepath.Join(s.config.LocalDir, filepath.FromSlash(path))
		if mkErr := os.MkdirAll(filepath.Dir(dstPath), 0755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(dstPath, data, 0644)
	})
}

// cleanLocalGhosts removes local files not present in the remote hash map.
func (s *syncService) cleanLocalGhosts(xxhashMap map[string]string) {
	filepath.WalkDir(s.config.LocalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(s.config.LocalDir, path)
		if relErr != nil {
			return nil
		}
		if _, exists := xxhashMap[filepath.ToSlash(rel)]; !exists {
			os.Remove(path)
		}
		return nil
	})
}

// stageUpload uploads files from local storage to remote staging prefix.
func (s *syncService) stageUpload(ctx context.Context, files []string) error {
	for i, file := range files {
		s.send(ports.UpdateInfo{
			Operation: "sync-" + s.config.Prefix,
			Message:   "Uploading",
			Data:      map[string]any{"file": file, "progress": i + 1, "total": len(files)},
		})
		srcKey := s.config.Prefix + "/" + file
		data, err := s.local.Get(ctx, srcKey)
		if err != nil {
			return fmt.Errorf("get local %s: %w", file, err)
		}
		dstKey := s.remoteStaging + "/" + file
		if err := s.remote.Put(ctx, dstKey, data); err != nil {
			return fmt.Errorf("stage %s: %w", file, err)
		}
	}
	return nil
}

// commitUpload moves files from remote staging to final remote prefix.
func (s *syncService) commitUpload(ctx context.Context, files []string) error {
	for _, file := range files {
		src := s.remoteStaging + "/" + file
		dst := s.config.Prefix + "/" + file
		if err := s.remote.Copy(ctx, src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", file, err)
		}
		_ = s.remote.Delete(ctx, src)
	}
	return nil
}

// cleanRemoteOrphans batch-deletes orphaned files from remote.
func (s *syncService) cleanRemoteOrphans(ctx context.Context, files []string) {
	keys := make([]string, len(files))
	for i, file := range files {
		keys[i] = s.config.Prefix + "/" + file
	}
	_ = s.remote.DeleteBatch(ctx, keys)
}

// cleanRemoteStaging batch-deletes remaining staging keys.
func (s *syncService) cleanRemoteStaging(ctx context.Context) {
	if s.remote == nil {
		return
	}
	keys, err := s.remote.List(ctx, s.remoteStaging)
	if err == nil && len(keys) > 0 {
		_ = s.remote.DeleteBatch(ctx, keys)
	}
}
