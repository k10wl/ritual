// Package services hosts the SyncService façade that drives the sync state
// machine via internal/core/sync. The public Upload/Download API is preserved
// for callers (heartbeat supervisor, integration tests) — internals are
// state-machine-driven so every run emits the Sync*Info event family.
package services

import (
	"context"
	"fmt"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"time"

	syncpkg "ritual/internal/core/sync"
)

// SyncConfig holds the identity of a sync target (e.g. "worlds", "server").
type SyncConfig struct {
	Prefix   string // key prefix on both src and dst storages (e.g. "worlds")
	LocalDir string // absolute path to final destination on local FS
}

// SyncService wraps sync.Run with prefix mapping so callers operate on
// un-prefixed file paths (matching what scanners + manifests use). Upload
// scans the local tree; Download is driven by the remote manifest.
type SyncService struct {
	scanner ports.DirectoryScanner
	local   ports.StorageRepository
	remote  ports.StorageRepository
	bus     ports.EventBus
	config  SyncConfig
}

var _ ports.SyncService = (*SyncService)(nil)

// NewSyncService constructs a SyncService. The localStaging/remoteStaging
// arguments are ignored — the underlying engine generates a UUIDv4 staging
// path per run. Kept as parameters for call-site compatibility.
func NewSyncService(
	scanner ports.DirectoryScanner,
	local, remote ports.StorageRepository,
	events ports.EventBus,
	config SyncConfig,
	_localStaging string, //nolint:revive // back-compat parameter
	_remoteStaging string, //nolint:revive // back-compat parameter
) *SyncService {
	return &SyncService{
		scanner: scanner,
		local:   local,
		remote:  remote,
		bus:     events,
		config:  config,
	}
}

// Download pulls files from remote into local. The new local SyncState
// mirrors the remote map on success.
func (s *SyncService) Download(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error) {
	srcMap := prefixMap(remote.XXHashMap, s.config.Prefix)
	dstMap := prefixMap(local.XXHashMap, s.config.Prefix)

	rs, err := syncpkg.Run(ctx, s.remote, s.local, srcMap, dstMap, syncpkg.DirectionDownload, s.bus)
	if err != nil {
		return local, fmt.Errorf("download %s: %w", s.config.Prefix, err)
	}
	if rs.Err != nil {
		return local, rs.Err
	}

	return domain.SyncState{
		XXHashMap:    cloneMap(remote.XXHashMap),
		XXHashSyncAt: remote.XXHashSyncAt,
	}, nil
}

// Upload pushes locally-scanned files to remote. The new SyncState carries
// the freshly scanned map and a current timestamp.
func (s *SyncService) Upload(ctx context.Context, local, remote domain.SyncState) (domain.SyncState, error) {
	if s.scanner == nil {
		return local, fmt.Errorf("upload %s: scanner not wired", s.config.Prefix)
	}

	scanned, err := s.scanner.Scan(ctx)
	if err != nil {
		return local, fmt.Errorf("upload %s: scan: %w", s.config.Prefix, err)
	}
	now := time.Now()

	srcMap := prefixMap(scanned, s.config.Prefix)
	dstMap := prefixMap(remote.XXHashMap, s.config.Prefix)

	rs, err := syncpkg.Run(ctx, s.local, s.remote, srcMap, dstMap, syncpkg.DirectionUpload, s.bus)
	if err != nil {
		return local, fmt.Errorf("upload %s: %w", s.config.Prefix, err)
	}
	if rs.Err != nil {
		return local, rs.Err
	}

	return domain.SyncState{XXHashMap: scanned, XXHashSyncAt: now}, nil
}

// prefixMap returns a copy of m with each key prefixed by prefix + "/".
// nil prefix returns a clone with original keys.
func prefixMap(m map[string]domain.FileEntry, prefix string) map[string]domain.FileEntry {
	if prefix == "" {
		return cloneMap(m)
	}
	out := make(map[string]domain.FileEntry, len(m))
	for k, v := range m {
		out[prefix+"/"+k] = v
	}
	return out
}

func cloneMap(m map[string]domain.FileEntry) map[string]domain.FileEntry {
	out := make(map[string]domain.FileEntry, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
