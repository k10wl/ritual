package config_test

import (
	"ritual/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveWorkRoot_EmptyFallsBackToRootPath(t *testing.T) {
	original := config.RootPath
	config.RootPath = "/tmp/ritual-root-fixture"
	defer func() { config.RootPath = original }()

	assert.Equal(t, config.RootPath, config.ResolveWorkRoot(""), "an empty work_root must resolve to RootPath — today's single-root layout, zero config drift")
}

func TestResolveWorkRoot_AbsolutePathIsUsedAsIs(t *testing.T) {
	original := config.RootPath
	config.RootPath = "/tmp/ritual-root-fixture"
	defer func() { config.RootPath = original }()

	assert.Equal(t, "/tmp/ritual-content-fixture", config.ResolveWorkRoot("/tmp/ritual-content-fixture"), "an explicit absolute work_root must be used as-is")
}

func TestResolveWorkRoot_RelativePathFallsBackToRootPath(t *testing.T) {
	original := config.RootPath
	config.RootPath = "/tmp/ritual-root-fixture"
	defer func() { config.RootPath = original }()

	assert.Equal(t, config.RootPath, config.ResolveWorkRoot("relative/path"), "a relative work_root is not rejected at this layer (domain.Settings.Validate owns that) — it must fall back to RootPath rather than being used verbatim")
}
