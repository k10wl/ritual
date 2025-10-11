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
		InstanceVersion: "test-instance",
		RitualVersion:   "1.0.0",
	}

	mockLibrarian := mock.(*MockLibrarianService)
	mockLibrarian.GetLocalManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return testManifest, nil
	}

	result, err := librarian.GetLocalManifest(context.Background())
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.InstanceVersion != testManifest.InstanceVersion {
		t.Errorf("Expected instance %s, got %s", testManifest.InstanceVersion, result.InstanceVersion)
	}

	mockLibrarian.GetRemoteManifestFunc = func(ctx context.Context) (*domain.Manifest, error) {
		return testManifest, nil
	}

	result, err = librarian.GetRemoteManifest(context.Background())
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.InstanceVersion != testManifest.InstanceVersion {
		t.Errorf("Expected instance %s, got %s", testManifest.InstanceVersion, result.InstanceVersion)
	}

	mockLibrarian.SaveLocalManifestFunc = func(ctx context.Context, manifest *domain.Manifest) error {
		if manifest.InstanceVersion != testManifest.InstanceVersion {
			t.Errorf("Expected instance %s, got %s", testManifest.InstanceVersion, manifest.InstanceVersion)
		}
		return nil
	}

	err = librarian.SaveLocalManifest(context.Background(), testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockLibrarian.SaveRemoteManifestFunc = func(ctx context.Context, manifest *domain.Manifest) error {
		if manifest.InstanceVersion != testManifest.InstanceVersion {
			t.Errorf("Expected instance %s, got %s", testManifest.InstanceVersion, manifest.InstanceVersion)
		}
		return nil
	}

	err = librarian.SaveRemoteManifest(context.Background(), testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
