package mocks

import (
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
	mockLibrarian.GetLocalManifestFunc = func() (*domain.Manifest, error) {
		return testManifest, nil
	}

	result, err := librarian.GetLocalManifest()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.InstanceID != testManifest.InstanceID {
		t.Errorf("Expected instance %s, got %s", testManifest.InstanceID, result.InstanceID)
	}

	mockLibrarian.GetRemoteManifestFunc = func() (*domain.Manifest, error) {
		return testManifest, nil
	}

	result, err = librarian.GetRemoteManifest()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result.InstanceID != testManifest.InstanceID {
		t.Errorf("Expected instance %s, got %s", testManifest.InstanceID, result.InstanceID)
	}

	mockLibrarian.SaveLocalManifestFunc = func(manifest *domain.Manifest) error {
		if manifest.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected instance %s, got %s", testManifest.InstanceID, manifest.InstanceID)
		}
		return nil
	}

	err = librarian.SaveLocalManifest(testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockLibrarian.SaveRemoteManifestFunc = func(manifest *domain.Manifest) error {
		if manifest.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected instance %s, got %s", testManifest.InstanceID, manifest.InstanceID)
		}
		return nil
	}

	err = librarian.SaveRemoteManifest(testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
