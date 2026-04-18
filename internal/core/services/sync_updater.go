package services

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// SyncDownloadUpdater wraps SyncService.Download as an UpdaterService.
// Reads both manifests via ManifestStore, runs sync.Download on the selected
// SyncState pair, writes the new state back to the local manifest.
type SyncDownloadUpdater struct {
	sync     *SyncService
	local    ports.ManifestStore
	remote   ports.ManifestStore
	getState func(*domain.Manifest) *domain.SyncState
}

var _ ports.UpdaterService = (*SyncDownloadUpdater)(nil)

// NewSyncDownloadUpdater creates an updater that runs sync download.
// getState selects which SyncState to operate on (e.g. &manifest.Worlds.SyncState or &manifest.Server.SyncState).
func NewSyncDownloadUpdater(
	sync *SyncService,
	local, remote ports.ManifestStore,
	getState func(*domain.Manifest) *domain.SyncState,
) *SyncDownloadUpdater {
	return &SyncDownloadUpdater{sync: sync, local: local, remote: remote, getState: getState}
}

// Run executes the download sync.
func (u *SyncDownloadUpdater) Run(ctx context.Context) error {
	localManifest, err := u.local.Get(ctx)
	if err != nil {
		return err
	}
	remoteManifest, err := u.remote.Get(ctx)
	if err != nil {
		return err
	}

	localState := u.getState(localManifest)
	remoteState := u.getState(remoteManifest)

	if localState.XXHashSyncAt.After(remoteState.XXHashSyncAt) {
		return nil
	}

	newState, err := u.sync.Download(ctx, *localState, *remoteState)
	if err != nil {
		return err
	}

	*localState = newState
	return u.local.Save(ctx, localManifest)
}

// SyncUploader wraps SyncService.Upload as an UpdaterService for exit flow.
type SyncUploader struct {
	sync     *SyncService
	local    ports.ManifestStore
	remote   ports.ManifestStore
	getState func(*domain.Manifest) *domain.SyncState
}

var _ ports.UpdaterService = (*SyncUploader)(nil)

// NewSyncUploader builds a SyncUploader targeting the directory whose SyncState the selector returns.
func NewSyncUploader(
	sync *SyncService,
	local, remote ports.ManifestStore,
	getState func(*domain.Manifest) *domain.SyncState,
) *SyncUploader {
	return &SyncUploader{sync: sync, local: local, remote: remote, getState: getState}
}

// Run uploads local changes and persists the resulting SyncState to both manifests.
func (u *SyncUploader) Run(ctx context.Context) error {
	localManifest, err := u.local.Get(ctx)
	if err != nil {
		return err
	}
	remoteManifest, err := u.remote.Get(ctx)
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

	if err := u.local.Save(ctx, localManifest); err != nil {
		return err
	}
	return u.remote.Save(ctx, remoteManifest)
}
