package services

import (
	"context"

	"ritual/internal/core/ports"
)

// SyncDownloadUpdater wraps SyncService.Download as an UpdaterService.
// Used in Molfar's prepare phase to download world changes from remote.
type SyncDownloadUpdater struct {
	sync *SyncService
}

var _ ports.UpdaterService = (*SyncDownloadUpdater)(nil)

// NewSyncDownloadUpdater creates an updater that runs sync download.
func NewSyncDownloadUpdater(sync *SyncService) *SyncDownloadUpdater {
	return &SyncDownloadUpdater{sync: sync}
}

// Run executes the download sync.
func (u *SyncDownloadUpdater) Run(ctx context.Context) error {
	return u.sync.Download(ctx)
}

// SyncUploadBackupper wraps SyncService.Upload as a BackupperService.
// Used in Molfar's exit phase to upload world changes to remote.
type SyncUploadBackupper struct {
	sync *SyncService
}

var _ ports.BackupperService = (*SyncUploadBackupper)(nil)

// NewSyncUploadBackupper creates a backupper that runs sync upload.
func NewSyncUploadBackupper(sync *SyncService) *SyncUploadBackupper {
	return &SyncUploadBackupper{sync: sync}
}

// Run executes the upload sync.
func (b *SyncUploadBackupper) Run(ctx context.Context) (string, error) {
	return "", b.sync.Upload(ctx)
}
