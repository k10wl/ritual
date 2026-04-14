package services_test

import (
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/services"
)

func TestShouldBackup_EqualMaps_False(t *testing.T) {
	local := domain.SyncState{XXHashMap: map[string]string{"a": "h1", "b": "h2"}}
	remote := domain.SyncState{XXHashMap: map[string]string{"a": "h1", "b": "h2"}}
	if services.ShouldBackup(local, remote) {
		t.Error("equal maps: want false, got true")
	}
}

func TestShouldBackup_DifferentMaps_True(t *testing.T) {
	local := domain.SyncState{XXHashMap: map[string]string{"a": "h1"}}
	remote := domain.SyncState{XXHashMap: map[string]string{"a": "h2"}}
	if !services.ShouldBackup(local, remote) {
		t.Error("different hashes: want true, got false")
	}
}

func TestShouldBackup_LocalEmpty_True(t *testing.T) {
	local := domain.SyncState{}
	remote := domain.SyncState{XXHashMap: map[string]string{"a": "h1"}}
	if !services.ShouldBackup(local, remote) {
		t.Error("local empty: want true, got false")
	}
}

func TestShouldBackup_BothEmpty_False(t *testing.T) {
	if services.ShouldBackup(domain.SyncState{}, domain.SyncState{}) {
		t.Error("both empty: want false, got true")
	}
}
