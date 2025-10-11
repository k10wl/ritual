package mocks

import (
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"testing"
)

func TestMockValidatorService(t *testing.T) {
	mock := NewMockValidatorService()

	var validator ports.ValidatorService = mock
	if validator == nil {
		t.Error("MockValidatorService does not implement ValidatorService interface")
	}

	testManifest := &domain.Manifest{
		InstanceID: "test-instance",
		Version:    "1.0.0",
	}

	mockValidator := mock.(*MockValidatorService)
	mockValidator.CheckInstanceFunc = func(local *domain.Manifest, remote *domain.Manifest) error {
		if local.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected local instance %s, got %s", testManifest.InstanceID, local.InstanceID)
		}
		if remote.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected remote instance %s, got %s", testManifest.InstanceID, remote.InstanceID)
		}
		return nil
	}

	err := validator.CheckInstance(testManifest, testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockValidator.CheckWorldFunc = func(local *domain.Manifest, remote *domain.Manifest) error {
		if local.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected local instance %s, got %s", testManifest.InstanceID, local.InstanceID)
		}
		if remote.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected remote instance %s, got %s", testManifest.InstanceID, remote.InstanceID)
		}
		return nil
	}

	err = validator.CheckWorld(testManifest, testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockValidator.CheckLockFunc = func(local *domain.Manifest, remote *domain.Manifest) error {
		if local.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected local instance %s, got %s", testManifest.InstanceID, local.InstanceID)
		}
		if remote.InstanceID != testManifest.InstanceID {
			t.Errorf("Expected remote instance %s, got %s", testManifest.InstanceID, remote.InstanceID)
		}
		return nil
	}

	err = validator.CheckLock(testManifest, testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
