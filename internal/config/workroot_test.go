package config_test

import (
	"path/filepath"
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

	// t.TempDir() rather than a hardcoded "/tmp/..." string: filepath.IsAbs
	// requires a drive letter on Windows, so a Unix-style literal is never
	// absolute there and this assertion would fail on every Windows runner.
	absoluteContentPath := filepath.Join(t.TempDir(), "ritual-content-fixture")
	assert.Equal(t, absoluteContentPath, config.ResolveWorkRoot(absoluteContentPath), "an explicit absolute work_root must be used as-is")
}

func TestResolveWorkRoot_RelativePathFallsBackToRootPath(t *testing.T) {
	original := config.RootPath
	config.RootPath = "/tmp/ritual-root-fixture"
	defer func() { config.RootPath = original }()

	assert.Equal(t, config.RootPath, config.ResolveWorkRoot("relative/path"), "a relative work_root is not rejected at this layer (domain.Settings.Validate owns that) — it must fall back to RootPath rather than being used verbatim")
}
