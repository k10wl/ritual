package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWorld(t *testing.T) {
	uri := "test://world"
	createdAt := time.Now()

	world := &World{
		URI:       uri,
		CreatedAt: createdAt,
	}

	assert.Equal(t, uri, world.URI)
	assert.Equal(t, createdAt, world.CreatedAt)
}

func TestNewWorld(t *testing.T) {
	uri := "test://world"
	world, err := NewWorld(uri)

	assert.NoError(t, err)
	assert.NotNil(t, world)
	assert.Equal(t, uri, world.URI)
	assert.False(t, world.CreatedAt.IsZero(), "Expected CreatedAt to be set")
}

func TestNewWorldEmptyURI(t *testing.T) {
	world, err := NewWorld("")

	assert.Error(t, err, "Expected error for empty URI")
	assert.Nil(t, world, "Expected world to be nil for empty URI")
}
