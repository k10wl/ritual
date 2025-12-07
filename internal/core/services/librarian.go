package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

var (
	ErrEmptyData   = errors.New("empty data")
	ErrNilManifest = errors.New("nil manifest")
)

// LibrarianService implements manifest management and synchronization
type LibrarianService struct {
	localStorage  ports.StorageRepository
	remoteStorage ports.StorageRepository
}

// NewLibrarianService creates a new LibrarianService instance
func NewLibrarianService(localStorage ports.StorageRepository, remoteStorage ports.StorageRepository) (*LibrarianService, error) {
	if localStorage == nil {
		return nil, fmt.Errorf("localStorage cannot be nil")
	}
	if remoteStorage == nil {
		return nil, fmt.Errorf("remoteStorage cannot be nil")
	}
	return &LibrarianService{
		localStorage:  localStorage,
		remoteStorage: remoteStorage,
	}, nil
}

// GetLocalManifest retrieves the local manifest
func (l *LibrarianService) GetLocalManifest(ctx context.Context) (*domain.Manifest, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}
	if l.localStorage == nil {
		return nil, fmt.Errorf("localStorage repository is nil")
	}
	data, err := l.localStorage.Get(ctx, config.ManifestFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to get local manifest: %w", err)
	}

	if len(data) == 0 {
		return nil, ErrEmptyData
	}

	var manifest domain.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local manifest: %w", err)
	}

	return &manifest, nil
}

// GetRemoteManifest retrieves the remote manifest
func (l *LibrarianService) GetRemoteManifest(ctx context.Context) (*domain.Manifest, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}
	if l.remoteStorage == nil {
		return nil, fmt.Errorf("remoteStorage repository is nil")
	}
	data, err := l.remoteStorage.Get(ctx, config.ManifestFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote manifest: %w", err)
	}

	if len(data) == 0 {
		return nil, ErrEmptyData
	}

	var manifest domain.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remote manifest: %w", err)
	}

	return &manifest, nil
}

// SaveLocalManifest stores the manifest locally
func (l *LibrarianService) SaveLocalManifest(ctx context.Context, manifest *domain.Manifest) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	if l.localStorage == nil {
		return fmt.Errorf("localStorage repository is nil")
	}
	if manifest == nil {
		return ErrNilManifest
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := l.localStorage.Put(ctx, config.ManifestFilename, data); err != nil {
		return fmt.Errorf("failed to save local manifest: %w", err)
	}

	return nil
}

// SaveRemoteManifest stores the manifest remotely
func (l *LibrarianService) SaveRemoteManifest(ctx context.Context, manifest *domain.Manifest) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	if l.remoteStorage == nil {
		return fmt.Errorf("remoteStorage repository is nil")
	}
	if manifest == nil {
		return ErrNilManifest
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := l.remoteStorage.Put(ctx, config.ManifestFilename, data); err != nil {
		return fmt.Errorf("failed to save remote manifest: %w", err)
	}

	return nil
}
