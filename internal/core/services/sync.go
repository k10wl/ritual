package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

var (
	ErrSyncScannerNil   = errors.New("world scanner cannot be nil")
	ErrSyncLocalNil     = errors.New("local storage cannot be nil")
	ErrSyncRemoteNil    = errors.New("remote storage cannot be nil")
	ErrSyncLibrarianNil = errors.New("librarian service cannot be nil")
	ErrSyncNil          = errors.New("sync service cannot be nil")
)

// SyncPhase represents one atomic step in a sync operation.
type SyncPhase interface {
	Execute(ctx context.Context) error
	Verify(ctx context.Context) error
	Name() string
}

// SyncService orchestrates delta sync as a state machine.
// Each operation builds a chain of SyncPhase implementations and runs them sequentially.
type SyncService struct {
	scanner    ports.DirectoryScanner
	local      ports.StorageRepository
	remote     ports.StorageRepository
	librarian  ports.LibrarianService
	events     chan<- ports.Event
	worldsRoot string
	lockID     string
}

func NewSyncService(
	scanner ports.DirectoryScanner,
	local ports.StorageRepository,
	remote ports.StorageRepository,
	librarian ports.LibrarianService,
	events chan<- ports.Event,
	worldsRoot string,
	lockID string,
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
		lockID:     lockID,
	}, nil
}

func (s *SyncService) send(evt ports.Event) {
	ports.SendEvent(s.events, evt)
}

func (s *SyncService) runPhases(ctx context.Context, phases []SyncPhase) error {
	for _, p := range phases {
		s.send(ports.StartEvent{Operation: p.Name()})
		if err := p.Execute(ctx); err != nil {
			return fmt.Errorf("%s: %w", p.Name(), err)
		}
		if err := p.Verify(ctx); err != nil {
			return fmt.Errorf("%s verification: %w", p.Name(), err)
		}
		s.send(ports.FinishEvent{Operation: p.Name()})
	}
	return nil
}

// Download fetches changed files from remote, stages locally, commits to worlds, cleans ghosts.
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

	syncPrefix := "sync/" + s.lockID

	phases := []SyncPhase{
		&StagePhase{
			source:     s.remote,
			dest:       s.local,
			files:      diff.Download,
			syncPrefix: syncPrefix,
			worldsKey:  "worlds",
			events:     s.events,
			operation:  "sync-download",
		},
		&CommitDownloadPhase{
			local:          s.local,
			librarian:      s.librarian,
			files:          diff.Download,
			syncPrefix:     syncPrefix,
			localManifest:  localManifest,
			remoteManifest: remoteManifest,
		},
		&CleanupDownloadPhase{
			local:      s.local,
			worldsRoot: s.worldsRoot,
			xxhashMap:  remoteManifest.XXHashMap,
			syncPrefix: syncPrefix,
		},
	}

	if err := s.runPhases(ctx, phases); err != nil {
		return err
	}

	s.send(ports.FinishEvent{Operation: "sync-download"})
	return nil
}

// Upload scans local worlds, stages changes to remote, commits to remote worlds, deletes orphans.
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

	syncPrefix := "sync/" + s.lockID

	var phases []SyncPhase

	if len(diff.Upload) > 0 {
		phases = append(phases,
			&StagePhase{
				source:     s.local,
				dest:       s.remote,
				files:      diff.Upload,
				syncPrefix: syncPrefix,
				worldsKey:  "worlds",
				events:     s.events,
				operation:  "sync-upload",
			},
			&CommitUploadPhase{
				remote:         s.remote,
				librarian:      s.librarian,
				files:          diff.Upload,
				syncPrefix:     syncPrefix,
				newMap:         newMap,
				syncAt:         localManifest.XXHashSyncAt,
				remoteManifest: remoteManifest,
			},
		)
	} else {
		// Delete-only: still need manifest update
		phases = append(phases, &CommitUploadPhase{
			remote:         s.remote,
			librarian:      s.librarian,
			files:          nil,
			syncPrefix:     syncPrefix,
			newMap:         newMap,
			syncAt:         localManifest.XXHashSyncAt,
			remoteManifest: remoteManifest,
		})
	}

	phases = append(phases, &CleanupUploadPhase{
		remote:     s.remote,
		files:      diff.Delete,
		syncPrefix: syncPrefix,
		events:     s.events,
	})

	if err := s.runPhases(ctx, phases); err != nil {
		return err
	}

	s.send(ports.FinishEvent{Operation: "sync-upload"})
	return nil
}
