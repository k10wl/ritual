package services

import (
	"context"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// SyncDownloadUpdater wraps syncService.Download as an UpdaterService.
// Used in Molfar's prepare phase to download changes from remote.
type SyncDownloadUpdater struct {
	sync      *syncService
	librarian ports.LibrarianService
	getState  func(*domain.Manifest) *domain.SyncState
}

var _ ports.UpdaterService = (*SyncDownloadUpdater)(nil)

// NewSyncDownloadUpdater creates an updater that runs sync download.
// getState selects which SyncState to operate on (e.g. &manifest.Worlds.SyncState or &manifest.Server.SyncState).
func NewSyncDownloadUpdater(sync *syncService, librarian ports.LibrarianService, getState func(*domain.Manifest) *domain.SyncState) *SyncDownloadUpdater {
	return &SyncDownloadUpdater{sync: sync, librarian: librarian, getState: getState}
}

// Run executes the download sync: reads manifests, calls Download with value semantics, writes result back.
func (u *SyncDownloadUpdater) Run(ctx context.Context) error {
	localManifest, err := u.librarian.GetLocalManifest(ctx)
	if err != nil {
		return err
	}
	remoteManifest, err := u.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return err
	}

	localState := u.getState(localManifest)
	remoteState := u.getState(remoteManifest)

	newState, err := u.sync.Download(ctx, *localState, *remoteState)
	if err != nil {
		return err
	}

	*localState = newState
	return u.librarian.SaveLocalManifest(ctx, localManifest)
}

// SyncUploader wraps syncService.Upload as an UpdaterService for exit flow.
type SyncUploader struct {
	sync      *syncService
	librarian ports.LibrarianService
	getState  func(*domain.Manifest) *domain.SyncState
}

var _ ports.UpdaterService = (*SyncUploader)(nil)

func NewSyncUploader(sync *syncService, librarian ports.LibrarianService, getState func(*domain.Manifest) *domain.SyncState) *SyncUploader {
	return &SyncUploader{sync: sync, librarian: librarian, getState: getState}
}

func (u *SyncUploader) Run(ctx context.Context) error {
	localManifest, err := u.librarian.GetLocalManifest(ctx)
	if err != nil {
		return err
	}
	remoteManifest, err := u.librarian.GetRemoteManifest(ctx)
	if err != nil {
		return err
	}

	localState := u.getState(localManifest)
	remoteState := u.getState(remoteManifest)

	newState, err := u.sync.Upload(ctx, *localState, *remoteState)
	if err != nil {
		return err
	}

	*localState = newState
	*remoteState = newState

	if err := u.librarian.SaveLocalManifest(ctx, localManifest); err != nil {
		return err
	}
	return u.librarian.SaveRemoteManifest(ctx, remoteManifest)
}
