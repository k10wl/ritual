package services

import (
	"context"
	"fmt"
	"path/filepath"

	"ritual/internal/core/ports"
)

// StagePhase transfers files from source to dest sync staging area.
// Used by both upload (local→remote) and download (remote→local).
type StagePhase struct {
	source     ports.StorageRepository
	dest       ports.StorageRepository
	files      []string
	syncPrefix string
	worldsKey  string
	events     chan<- ports.Event
	operation  string
}

func (p *StagePhase) Name() string { return p.operation + "-stage" }

func (p *StagePhase) Execute(ctx context.Context) error {
	ports.SendEvent(p.events, ports.UpdateEvent{
		Operation: p.operation,
		Message:   "Staging files",
		Data:      map[string]any{"total_files": len(p.files)},
	})

	for i, filePath := range p.files {
		ports.SendEvent(p.events, ports.UpdateEvent{
			Operation: p.operation,
			Message:   "Transferring",
			Data:      map[string]any{"file": filePath, "progress": i + 1, "total": len(p.files)},
		})

		srcKey := filepath.ToSlash(filepath.Join(p.worldsKey, filePath))
		dstKey := filepath.ToSlash(filepath.Join(p.syncPrefix, filePath))

		data, err := p.source.Get(ctx, srcKey)
		if err != nil {
			return fmt.Errorf("get %s: %w", filePath, err)
		}
		if err := p.dest.Put(ctx, dstKey, data); err != nil {
			return fmt.Errorf("stage %s: %w", filePath, err)
		}
	}
	return nil
}

func (p *StagePhase) Verify(ctx context.Context) error {
	for _, filePath := range p.files {
		key := filepath.ToSlash(filepath.Join(p.syncPrefix, filePath))
		if _, err := p.dest.Get(ctx, key); err != nil {
			return fmt.Errorf("missing staged file %s: %w", filePath, err)
		}
	}
	return nil
}
