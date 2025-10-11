package mocks

import (
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"testing"
	"time"
)

func TestMockValidatorService(t *testing.T) {
	mock := &MockValidatorService{}

	var validator ports.ValidatorService = mock
	if validator == nil {
		t.Error("MockValidatorService does not implement ValidatorService interface")
	}

	testManifest := &domain.Manifest{
		InstanceVersion: "1.0.0",
		RitualVersion:   "1.0.0",
		UpdatedAt:       time.Now(),
	}

	mock.CheckInstanceFunc = func(local *domain.Manifest, remote *domain.Manifest) error {
		if local.InstanceVersion != testManifest.InstanceVersion {
			t.Errorf("Expected local instance %s, got %s", testManifest.InstanceVersion, local.InstanceVersion)
		}
		if remote.InstanceVersion != testManifest.InstanceVersion {
			t.Errorf("Expected remote instance %s, got %s", testManifest.InstanceVersion, remote.InstanceVersion)
		}
		return nil
	}

	err := validator.CheckInstance(testManifest, testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mock.CheckWorldFunc = func(local *domain.Manifest, remote *domain.Manifest) error {
		if local.InstanceVersion != testManifest.InstanceVersion {
			t.Errorf("Expected local instance %s, got %s", testManifest.InstanceVersion, local.InstanceVersion)
		}
		if remote.InstanceVersion != testManifest.InstanceVersion {
			t.Errorf("Expected remote instance %s, got %s", testManifest.InstanceVersion, remote.InstanceVersion)
		}
		return nil
	}

	err = validator.CheckWorld(testManifest, testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mock.CheckLockFunc = func(local *domain.Manifest, remote *domain.Manifest) error {
		if local.InstanceVersion != testManifest.InstanceVersion {
			t.Errorf("Expected local instance %s, got %s", testManifest.InstanceVersion, local.InstanceVersion)
		}
		if remote.InstanceVersion != testManifest.InstanceVersion {
			t.Errorf("Expected remote instance %s, got %s", testManifest.InstanceVersion, remote.InstanceVersion)
		}
		return nil
	}

	err = validator.CheckLock(testManifest, testManifest)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
