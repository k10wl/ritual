package domain

import (
	"fmt"
	"path/filepath"
	"time"
)

// World represents a world data entity
type World struct {
	URI       string    `json:"uri"`
	CreatedAt time.Time `json:"created_at"`
}

// NewWorld creates a new World instance with validation
func NewWorld(uri string) (*World, error) {
	if uri == "" {
		return nil, fmt.Errorf("URI cannot be empty")
	}

	return &World{
		URI:       filepath.ToSlash(uri),
		CreatedAt: time.Now(),
	}, nil
}
