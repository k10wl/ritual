package services

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// CommitDownloadPhase moves files from local sync staging to local worlds
// and updates local manifest to match remote. Manifest save is the commit point.
type CommitDownloadPhase struct {
	local          ports.StorageRepository
	librarian      ports.LibrarianService
	files          []string
	syncPrefix     string
	localManifest  *domain.Manifest
	remoteManifest *domain.Manifest
}

func (p *CommitDownloadPhase) Name() string { return "sync-download-commit" }

func (p *CommitDownloadPhase) Execute(ctx context.Context) error {
	for _, filePath := range p.files {
		syncKey := filepath.ToSlash(filepath.Join(p.syncPrefix, filePath))
		data, err := p.local.Get(ctx, syncKey)
		if err != nil {
			return fmt.Errorf("read staged %s: %w", filePath, err)
		}
		if err := p.local.Put(ctx, filepath.ToSlash(filepath.Join("worlds", filePath)), data); err != nil {
			return fmt.Errorf("write %s: %w", filePath, err)
		}
		_ = p.local.Delete(ctx, syncKey)
	}

	p.localManifest.XXHashMap = p.remoteManifest.XXHashMap
	p.localManifest.XXHashSyncAt = p.remoteManifest.XXHashSyncAt
	return p.librarian.SaveLocalManifest(ctx, p.localManifest)
}

func (p *CommitDownloadPhase) Verify(ctx context.Context) error {
	return nil // manifest save is the atomic commit point
}

// CommitUploadPhase moves files from remote sync staging to remote worlds
// and updates remote manifest with new xxhash map. Manifest save is the commit point.
type CommitUploadPhase struct {
	remote         ports.StorageRepository
	librarian      ports.LibrarianService
	files          []string // nil for delete-only commits
	syncPrefix     string
	newMap         map[string]string
	syncAt         time.Time
	remoteManifest *domain.Manifest
}

func (p *CommitUploadPhase) Name() string { return "sync-upload-commit" }

func (p *CommitUploadPhase) Execute(ctx context.Context) error {
	for _, filePath := range p.files {
		src := filepath.ToSlash(filepath.Join(p.syncPrefix, filePath))
		dst := filepath.ToSlash(filepath.Join("worlds", filePath))
		if err := p.remote.Copy(ctx, src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", filePath, err)
		}
		_ = p.remote.Delete(ctx, src)
	}

	p.remoteManifest.XXHashMap = p.newMap
	p.remoteManifest.XXHashSyncAt = p.syncAt
	return p.librarian.SaveRemoteManifest(ctx, p.remoteManifest)
}

func (p *CommitUploadPhase) Verify(ctx context.Context) error {
	return nil // manifest save is the atomic commit point
}
