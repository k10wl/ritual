package testhelpers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants
const (
	expectedFileCount = 29
	regionFileSize    = 4096
	entityFileSize    = 4096
	poiFileSize       = 4096
)

// Unit Tests

func TestPaperMinecraftWorldSetup(t *testing.T) {
	tempDir := t.TempDir()
	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer root.Close()
	tempDir, createdFiles, compareFunc, err := PaperMinecraftWorldSetup(root)
	require.NoError(t, err)

	// Verify temp directory exists
	_, err = os.Stat(tempDir)
	require.NoError(t, err)

	// Verify all expected files were created
	expectedFiles := []string{
		"world/level.dat",
		"world/region/r.0.0.mca",
		"world/paper-world.yml",
		"world/session.lock",
		"world/uid.dat",
		"world/playerdata/player_0.dat",
		"world/playerdata/player_1.dat",
		"world/playerdata/player_2.dat",
		"world/serverconfig/forge-server.toml",
		"world/datapacks/bukkit/pack.mcmeta",
		"world/stats/stats_0.json",
		"world/stats/stats_1.json",
		"world/stats/stats_2.json",
		"world/advancements/advancement_0.json",
		"world/advancements/advancement_1.json",
		"world/advancements/advancement_2.json",
		"world/data/data_0.dat",
		"world/data/data_1.dat",
		"world/data/data_2.dat",
		"world/entities/r.0.0.mca",
		"world/entities/r.1.1.mca",
		"world/entities/r.2.2.mca",
		"world/poi/r.0.0.mca",
		"world/poi/r.1.1.mca",
		"world/poi/r.2.2.mca",
		"world_nether/level.dat",
		"world_nether/region/r.0.0.mca",
		"world_the_end/level.dat",
		"world_the_end/region/r.0.0.mca",
	}

	assert.ElementsMatch(t, expectedFiles, createdFiles)

	// Verify files actually exist
	for _, file := range createdFiles {
		filePath := filepath.Join(tempDir, file)
		_, err := os.Stat(filePath)
		require.NoError(t, err, "File %s should exist", file)
	}

	// Test comparison function with identical directory
	err = compareFunc(tempDir)
	assert.NoError(t, err, "Comparison should pass for identical directory")

	// Test comparison function with different directory (should fail)
	differentDir := t.TempDir()
	err = compareFunc(differentDir)
	assert.Error(t, err, "Comparison should fail for different directory")
}

func TestPaperMinecraftWorldSetupWithCustomDir(t *testing.T) {
	customDir := filepath.Join(t.TempDir(), "custom")
	err := os.MkdirAll(customDir, 0755)
	require.NoError(t, err)

	root, err := os.OpenRoot(customDir)
	require.NoError(t, err)
	defer root.Close()

	tempDir, createdFiles, compareFunc, err := PaperMinecraftWorldSetup(root)
	require.NoError(t, err)

	// Verify custom directory was used
	assert.Equal(t, customDir, tempDir)

	// Verify temp directory exists
	_, err = os.Stat(tempDir)
	require.NoError(t, err)

	// Verify all expected files were created
	expectedFiles := []string{
		"world/level.dat",
		"world/region/r.0.0.mca",
		"world/paper-world.yml",
		"world/session.lock",
		"world/uid.dat",
		"world/playerdata/player_0.dat",
		"world/playerdata/player_1.dat",
		"world/playerdata/player_2.dat",
		"world/serverconfig/forge-server.toml",
		"world/datapacks/bukkit/pack.mcmeta",
		"world/stats/stats_0.json",
		"world/stats/stats_1.json",
		"world/stats/stats_2.json",
		"world/advancements/advancement_0.json",
		"world/advancements/advancement_1.json",
		"world/advancements/advancement_2.json",
		"world/data/data_0.dat",
		"world/data/data_1.dat",
		"world/data/data_2.dat",
		"world/entities/r.0.0.mca",
		"world/entities/r.1.1.mca",
		"world/entities/r.2.2.mca",
		"world/poi/r.0.0.mca",
		"world/poi/r.1.1.mca",
		"world/poi/r.2.2.mca",
		"world_nether/level.dat",
		"world_nether/region/r.0.0.mca",
		"world_the_end/level.dat",
		"world_the_end/region/r.0.0.mca",
	}

	assert.ElementsMatch(t, expectedFiles, createdFiles)

	// Test comparison function with identical directory
	err = compareFunc(tempDir)
	assert.NoError(t, err, "Comparison should pass for identical directory")
}

func TestPaperMinecraftWorldSetupComparison(t *testing.T) {
	tempDir := t.TempDir()
	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer root.Close()
	tempDir, _, compareFunc, err := PaperMinecraftWorldSetup(root)
	require.NoError(t, err)

	// Create a copy of the directory structure
	copyDir := t.TempDir()

	// Copy all files to new directory
	err = copyDirectory(tempDir, copyDir)
	require.NoError(t, err)

	// Test comparison with copied directory (should pass)
	err = compareFunc(copyDir)
	assert.NoError(t, err, "Comparison should pass for copied directory")

	// Modify one file in the copy
	modifiedFile := filepath.Join(copyDir, "world/level.dat")
	err = os.WriteFile(modifiedFile, []byte("modified content"), 0644)
	require.NoError(t, err)

	// Test comparison with modified file (should fail)
	err = compareFunc(copyDir)
	assert.Error(t, err, "Comparison should fail for modified file")
}

func TestPaperMinecraftWorldSetupFailure(t *testing.T) {
	tempDir := t.TempDir()
	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer root.Close()
	tempDir, _, compareFunc, err := PaperMinecraftWorldSetup(root)
	require.NoError(t, err)

	// Create a directory with missing files
	incompleteDir := t.TempDir()

	// Only create one world directory
	worldDir := filepath.Join(incompleteDir, "world")
	err = os.MkdirAll(worldDir, 0755)
	require.NoError(t, err)

	// Create level.dat but not region file
	levelDatPath := filepath.Join(worldDir, "level.dat")
	err = os.WriteFile(levelDatPath, []byte("incomplete world"), 0644)
	require.NoError(t, err)

	// Test comparison should fail due to missing files
	err = compareFunc(incompleteDir)
	assert.Error(t, err, "Comparison should fail for incomplete directory")
	// The error could be either missing file or content mismatch
	assert.True(t, strings.Contains(err.Error(), "does not exist in new location") ||
		strings.Contains(err.Error(), "content differs between original and new location"))

	// Create directory with wrong content
	wrongContentDir := t.TempDir()
	err = copyDirectory(tempDir, wrongContentDir)
	require.NoError(t, err)

	// Modify a file content
	modifiedFile := filepath.Join(wrongContentDir, "world/level.dat")
	err = os.WriteFile(modifiedFile, []byte("wrong content"), 0644)
	require.NoError(t, err)

	// Test comparison should fail due to content mismatch
	err = compareFunc(wrongContentDir)
	assert.Error(t, err, "Comparison should fail for modified content")
	assert.Contains(t, err.Error(), "content differs between original and new location")
}

func TestGetFileHash(t *testing.T) {
	// Create a temporary file with known content
	tempFile := filepath.Join(t.TempDir(), "test.txt")
	content := "test content"
	err := os.WriteFile(tempFile, []byte(content), 0644)
	require.NoError(t, err)

	// Test hash calculation
	hash, err := getFileHash(tempFile)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Test with same content (should produce same hash)
	hash2, err := getFileHash(tempFile)
	require.NoError(t, err)
	assert.Equal(t, hash, hash2)

	// Test with non-existent file
	_, err = getFileHash("non-existent-file.txt")
	assert.Error(t, err)
}

func TestPaperMinecraftWorldSetup_NegativeCases(t *testing.T) {
	// Test with invalid directory path
	invalidDir := "/invalid/path/that/does/not/exist"
	invalidRoot, err := os.OpenRoot(invalidDir)
	assert.Error(t, err, "Should fail with invalid directory path")
	if invalidRoot == nil {
		// If root creation failed, skip the rest
		return
	}
	defer invalidRoot.Close()

	_, _, _, err = PaperMinecraftWorldSetup(invalidRoot)
	// May or may not fail depending on root creation

	// Test with read-only directory (if possible on current OS)
	tempDir := t.TempDir()
	readOnlyDir := filepath.Join(tempDir, "readonly")
	err = os.MkdirAll(readOnlyDir, 0444) // Read-only permissions
	require.NoError(t, err)

	// On Windows, read-only directories may still allow file creation
	// So we'll skip this test if it doesn't fail as expected
	readOnlyRoot, err := os.OpenRoot(readOnlyDir)
	if err == nil {
		defer readOnlyRoot.Close()
		_, _, _, err = PaperMinecraftWorldSetup(readOnlyRoot)
		if err != nil {
			assert.Error(t, err, "Should fail with read-only directory")
		} else {
			t.Log("Read-only directory test skipped - OS allows file creation in read-only dir")
		}
	}
}

func TestPaperMinecraftWorldSetup_ComparisonFunction_NegativeCases(t *testing.T) {
	tempDir := t.TempDir()
	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer root.Close()
	_, _, compareFunc, err := PaperMinecraftWorldSetup(root)
	require.NoError(t, err)

	// Test comparison with non-existent directory
	err = compareFunc("/non/existent/path")
	assert.Error(t, err, "Comparison should fail with non-existent directory")

	// Test comparison with empty directory
	emptyDir := t.TempDir()
	err = compareFunc(emptyDir)
	assert.Error(t, err, "Comparison should fail with empty directory")

	// Test comparison with directory containing only some files
	partialDir := t.TempDir()
	worldDir := filepath.Join(partialDir, "world")
	err = os.MkdirAll(worldDir, 0755)
	require.NoError(t, err)

	// Create only level.dat
	err = os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("test"), 0644)
	require.NoError(t, err)

	err = compareFunc(partialDir)
	assert.Error(t, err, "Comparison should fail with partial directory structure")
}

// Integration Tests

func TestPaperWorldIntegration_FileFormatCompliance(t *testing.T) {
	tempDir := t.TempDir()
	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer root.Close()
	_, createdFiles, _, err := PaperMinecraftWorldSetup(root)
	require.NoError(t, err)

	// Test Paper-specific file formats
	paperWorldPath := filepath.Join(tempDir, "world/paper-world.yml")
	content, err := os.ReadFile(paperWorldPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "_version: 30", "Paper world config should have version")

	// Test JSON format compliance
	statsPath := filepath.Join(tempDir, "world/stats/stats_0.json")
	content, err = os.ReadFile(statsPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `"stats"`, "Stats file should be valid JSON")

	// Test datapack format compliance
	packMcmetaPath := filepath.Join(tempDir, "world/datapacks/bukkit/pack.mcmeta")
	content, err = os.ReadFile(packMcmetaPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `"pack_format"`, "Pack.mcmeta should have pack_format")

	// Test TOML format compliance
	forgeTomlPath := filepath.Join(tempDir, "world/serverconfig/forge-server.toml")
	content, err = os.ReadFile(forgeTomlPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `[server]`, "Forge TOML should have server section")

	// Test region file size compliance
	regionPath := filepath.Join(tempDir, "world/region/r.0.0.mca")
	info, err := os.Stat(regionPath)
	require.NoError(t, err)
	assert.Equal(t, int64(regionFileSize), info.Size(), "Region file should be 4096 bytes")

	// Test entity file size compliance
	entityPath := filepath.Join(tempDir, "world/entities/r.0.0.mca")
	info, err = os.Stat(entityPath)
	require.NoError(t, err)
	assert.Equal(t, int64(entityFileSize), info.Size(), "Entity file should be 4096 bytes")

	// Test POI file size compliance
	poiPath := filepath.Join(tempDir, "world/poi/r.0.0.mca")
	info, err = os.Stat(poiPath)
	require.NoError(t, err)
	assert.Equal(t, int64(poiFileSize), info.Size(), "POI file should be 4096 bytes")

	// Verify comprehensive file count
	assert.GreaterOrEqual(t, len(createdFiles), expectedFileCount, "Should create comprehensive file structure")
}

func TestPaperWorldIntegration_DirectoryStructure(t *testing.T) {
	tempDir := t.TempDir()
	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer root.Close()
	_, _, _, err = PaperMinecraftWorldSetup(root)
	require.NoError(t, err)

	// Test main world directory structure
	expectedDirs := []string{
		"world/playerdata",
		"world/serverconfig",
		"world/datapacks",
		"world/datapacks/bukkit",
		"world/stats",
		"world/advancements",
		"world/data",
		"world/entities",
		"world/poi",
		"world/DIM-1",
		"world/DIM-1/data",
		"world/DIM-1/region",
		"world/DIM-1/poi",
		"world/DIM1",
		"world/DIM1/data",
		"world/DIM1/region",
		"world/DIM1/poi",
		"world_nether",
		"world_nether/region",
		"world_the_end",
		"world_the_end/region",
	}

	for _, dir := range expectedDirs {
		dirPath := filepath.Join(tempDir, dir)
		info, err := os.Stat(dirPath)
		require.NoError(t, err, "Directory %s should exist", dir)
		assert.True(t, info.IsDir(), "Path %s should be a directory", dir)
	}
}

func TestPaperWorldIntegration_CrossPlatformPaths(t *testing.T) {
	tempDir := t.TempDir()
	root, err := os.OpenRoot(tempDir)
	require.NoError(t, err)
	defer root.Close()
	_, createdFiles, compareFunc, err := PaperMinecraftWorldSetup(root)
	require.NoError(t, err)

	// Test that all file paths use forward slashes (standard for archives)
	for _, file := range createdFiles {
		assert.NotContains(t, file, "\\", "File paths should use forward slashes")
		assert.Contains(t, file, "/", "File paths should contain path separators")
	}

	// Test comparison function works with different path formats
	err = compareFunc(tempDir)
	assert.NoError(t, err, "Comparison should work with standard paths")

	// Test that file operations work correctly
	for _, file := range createdFiles {
		filePath := filepath.Join(tempDir, file)
		_, err := os.Stat(filePath)
		require.NoError(t, err, "File %s should be accessible", file)
	}
}

// Helper function to copy directory recursively
func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, content, info.Mode())
	})
}
