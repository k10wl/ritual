package domain

import (
	"testing"
	"time"
)

func TestWorld(t *testing.T) {
	uri := "test://world"
	createdAt := time.Now()

	world := &World{
		URI:       uri,
		CreatedAt: createdAt,
	}

	if world.URI != uri {
		t.Errorf("Expected URI %s, got %s", uri, world.URI)
	}

	if !world.CreatedAt.Equal(createdAt) {
		t.Errorf("Expected CreatedAt %v, got %v", createdAt, world.CreatedAt)
	}
}

func TestNewWorld(t *testing.T) {
	uri := "test://world"
	world, err := NewWorld(uri)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if world == nil {
		t.Error("Expected world to be created")
	}

	if world.URI != uri {
		t.Errorf("Expected URI %s, got %s", uri, world.URI)
	}

	if world.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
}

func TestNewWorldEmptyURI(t *testing.T) {
	world, err := NewWorld("")

	if err == nil {
		t.Error("Expected error for empty URI")
	}

	if world != nil {
		t.Error("Expected world to be nil for empty URI")
	}
}
