package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
func NewLibrarianService(localStorage ports.StorageRepository, remoteStorage ports.StorageRepository) *LibrarianService {
	return &LibrarianService{
		localStorage:  localStorage,
		remoteStorage: remoteStorage,
	}
}

// GetLocalManifest retrieves the local manifest
func (l *LibrarianService) GetLocalManifest() (*domain.Manifest, error) {
	ctx := context.Background()
	data, err := l.localStorage.Get(ctx, "manifest.json")
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
func (l *LibrarianService) GetRemoteManifest() (*domain.Manifest, error) {
	ctx := context.Background()
	data, err := l.remoteStorage.Get(ctx, "manifest.json")
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
func (l *LibrarianService) SaveLocalManifest(manifest *domain.Manifest) error {
	if manifest == nil {
		return ErrNilManifest
	}

	ctx := context.Background()
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := l.localStorage.Put(ctx, "manifest.json", data); err != nil {
		return fmt.Errorf("failed to save local manifest: %w", err)
	}

	return nil
}

// SaveRemoteManifest stores the manifest remotely
func (l *LibrarianService) SaveRemoteManifest(manifest *domain.Manifest) error {
	if manifest == nil {
		return ErrNilManifest
	}

	ctx := context.Background()
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := l.remoteStorage.Put(ctx, "manifest.json", data); err != nil {
		return fmt.Errorf("failed to save remote manifest: %w", err)
	}

	return nil
}
