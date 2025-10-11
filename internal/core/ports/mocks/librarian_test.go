package mocks

import (
	"context"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"testing"
)

func TestMockLibrarianService(t *testing.T) {
	mock := NewMockLibrarianService()

	var librarian ports.LibrarianService = mock
	if librarian == nil {
		t.Error("MockLibrarianService does not implement LibrarianService interface")
	}

	testManifest := &domain.Manifest{
		InstanceID: "test-instance",
		Version:    "1.0.0",
	}

	mockLibrarian := mock.(*MockLibrarianService)
	mockLibrarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return testManifest, nil
	}

	result, err := librarian.GetLocalManifest(context.Background())
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.InstanceID != testManifest.InstanceID {
		t.Errorf("Expected instance %s, got %s", testManifest.InstanceID, result.InstanceID)
	}

	mockLibrarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return testManifest, nil
	}

	result, err = librarian.GetRemoteManifest(context.Background())
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.InstanceID != testManifest.InstanceID {
		t.Errorf("Expected instance %s, got %s", testManifest.InstanceID, result.InstanceID)
	}

	mockLibrarian.SaveLocalManifestFunc = func(ctx context.Context, manifest *domain.Manifest) error {
		if manifest.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected instance %s, got %s", testManifest.InstanceID, manifest.InstanceID)
		}
		return nil
	}

	err = librarian.SaveLocalManifest(context.Background(), testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockLibrarian.SaveRemoteManifestFunc = func(ctx context.Context, manifest *domain.Manifest) error {
		if manifest.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected instance %s, got %s", testManifest.InstanceID, manifest.InstanceID)
		}
		return nil
	}

	err = librarian.SaveRemoteManifest(context.Background(), testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
