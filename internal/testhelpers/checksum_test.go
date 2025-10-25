package testhelpers_test

import (
	"os"
	"path/filepath"
	"ritual/internal/testhelpers"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDirectoryChecksum(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		tempDir := t.TempDir()

		checksum, err := testhelpers.DirectoryChecksum(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, checksum)
	})

	t.Run("single file", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test.txt")

		err := os.WriteFile(testFile, []byte("hello world"), 0644)
		assert.NoError(t, err)

		checksum, err := testhelpers.DirectoryChecksum(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, checksum)
	})

	t.Run("multiple files", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create multiple files
		files := map[string]string{
			"file1.txt": "content1",
			"file2.txt": "content2",
			"file3.txt": "content3",
		}

		for filename, content := range files {
			filePath := filepath.Join(tempDir, filename)
			err := os.WriteFile(filePath, []byte(content), 0644)
			assert.NoError(t, err)
		}

		checksum, err := testhelpers.DirectoryChecksum(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, checksum)
	})

	t.Run("nested directories", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create nested structure
		subDir := filepath.Join(tempDir, "subdir")
		err := os.Mkdir(subDir, 0755)
		assert.NoError(t, err)

		nestedFile := filepath.Join(subDir, "nested.txt")
		err = os.WriteFile(nestedFile, []byte("nested content"), 0644)
		assert.NoError(t, err)

		rootFile := filepath.Join(tempDir, "root.txt")
		err = os.WriteFile(rootFile, []byte("root content"), 0644)
		assert.NoError(t, err)

		checksum, err := testhelpers.DirectoryChecksum(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, checksum)
	})

	t.Run("consistent checksum for same content", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		// Create identical content in both directories
		files := map[string]string{
			"file1.txt": "same content",
			"file2.txt": "another content",
		}

		for filename, content := range files {
			filePath1 := filepath.Join(tempDir1, filename)
			filePath2 := filepath.Join(tempDir2, filename)

			err := os.WriteFile(filePath1, []byte(content), 0644)
			assert.NoError(t, err)
			err = os.WriteFile(filePath2, []byte(content), 0644)
			assert.NoError(t, err)
		}

		checksum1, err := testhelpers.DirectoryChecksum(tempDir1)
		assert.NoError(t, err)

		checksum2, err := testhelpers.DirectoryChecksum(tempDir2)
		assert.NoError(t, err)

		assert.Equal(t, checksum1, checksum2)
	})

	t.Run("different checksum for different content", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		// Create different content
		err := os.WriteFile(filepath.Join(tempDir1, "file.txt"), []byte("content1"), 0644)
		assert.NoError(t, err)

		err = os.WriteFile(filepath.Join(tempDir2, "file.txt"), []byte("content2"), 0644)
		assert.NoError(t, err)

		checksum1, err := testhelpers.DirectoryChecksum(tempDir1)
		assert.NoError(t, err)

		checksum2, err := testhelpers.DirectoryChecksum(tempDir2)
		assert.NoError(t, err)

		assert.NotEqual(t, checksum1, checksum2)
	})

	t.Run("error cases", func(t *testing.T) {
		t.Run("empty path", func(t *testing.T) {
			_, err := testhelpers.DirectoryChecksum("")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "directory path cannot be empty")
		})

		t.Run("non-existent directory", func(t *testing.T) {
			_, err := testhelpers.DirectoryChecksum("/non/existent/path")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "failed to stat directory")
		})

		t.Run("file instead of directory", func(t *testing.T) {
			tempDir := t.TempDir()
			testFile := filepath.Join(tempDir, "test.txt")

			err := os.WriteFile(testFile, []byte("test"), 0644)
			assert.NoError(t, err)

			_, err = testhelpers.DirectoryChecksum(testFile)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "is not a directory")
		})
	})
}

func TestCompareDirectories(t *testing.T) {
	t.Run("identical directories", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		// Create identical content
		files := map[string]string{
			"file1.txt": "content1",
			"file2.txt": "content2",
		}

		for filename, content := range files {
			err := os.WriteFile(filepath.Join(tempDir1, filename), []byte(content), 0644)
			assert.NoError(t, err)
			err = os.WriteFile(filepath.Join(tempDir2, filename), []byte(content), 0644)
			assert.NoError(t, err)
		}

		err := testhelpers.CompareDirectories(tempDir1, tempDir2, "test")
		assert.NoError(t, err)
	})

	t.Run("different directories", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		// Create different content
		err := os.WriteFile(filepath.Join(tempDir1, "file.txt"), []byte("content1"), 0644)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir2, "file.txt"), []byte("content2"), 0644)
		assert.NoError(t, err)

		err = testhelpers.CompareDirectories(tempDir1, tempDir2, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})

	t.Run("error in first directory", func(t *testing.T) {
		tempDir := t.TempDir()

		err := testhelpers.CompareDirectories("/non/existent", tempDir, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to calculate test checksum")
	})

	t.Run("error in second directory", func(t *testing.T) {
		tempDir := t.TempDir()

		err := testhelpers.CompareDirectories(tempDir, "/non/existent", "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to calculate test checksum")
	})
}

func TestCompareWorldDirectories(t *testing.T) {
	t.Run("identical world directories", func(t *testing.T) {
		localDir := t.TempDir()
		remoteDir := t.TempDir()

		worldDirs := []string{"world", "world_nether", "world_the_end"}

		// Create identical world directories
		for _, worldDir := range worldDirs {
			localWorldDir := filepath.Join(localDir, worldDir)
			remoteWorldDir := filepath.Join(remoteDir, worldDir)

			err := os.Mkdir(localWorldDir, 0755)
			assert.NoError(t, err)
			err = os.Mkdir(remoteWorldDir, 0755)
			assert.NoError(t, err)

			// Add identical content
			err = os.WriteFile(filepath.Join(localWorldDir, "level.dat"), []byte("level data"), 0644)
			assert.NoError(t, err)
			err = os.WriteFile(filepath.Join(remoteWorldDir, "level.dat"), []byte("level data"), 0644)
			assert.NoError(t, err)
		}

		err := testhelpers.CompareWorldDirectories(localDir, remoteDir, worldDirs)
		assert.NoError(t, err)
	})

	t.Run("different world directories", func(t *testing.T) {
		localDir := t.TempDir()
		remoteDir := t.TempDir()

		worldDirs := []string{"world"}

		// Create different content
		localWorldDir := filepath.Join(localDir, "world")
		remoteWorldDir := filepath.Join(remoteDir, "world")

		err := os.Mkdir(localWorldDir, 0755)
		assert.NoError(t, err)
		err = os.Mkdir(remoteWorldDir, 0755)
		assert.NoError(t, err)

		err = os.WriteFile(filepath.Join(localWorldDir, "level.dat"), []byte("level data 1"), 0644)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(remoteWorldDir, "level.dat"), []byte("level data 2"), 0644)
		assert.NoError(t, err)

		err = testhelpers.CompareWorldDirectories(localDir, remoteDir, worldDirs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})
}

func TestCompareInstanceDirectories(t *testing.T) {
	t.Run("identical instance directories", func(t *testing.T) {
		localDir := t.TempDir()
		remoteDir := t.TempDir()

		// Create identical content
		files := map[string]string{
			"server.properties": "server config",
			"eula.txt":          "eula content",
		}

		for filename, content := range files {
			err := os.WriteFile(filepath.Join(localDir, filename), []byte(content), 0644)
			assert.NoError(t, err)
			err = os.WriteFile(filepath.Join(remoteDir, filename), []byte(content), 0644)
			assert.NoError(t, err)
		}

		err := testhelpers.CompareInstanceDirectories(localDir, remoteDir)
		assert.NoError(t, err)
	})

	t.Run("different instance directories", func(t *testing.T) {
		localDir := t.TempDir()
		remoteDir := t.TempDir()

		// Create different content
		err := os.WriteFile(filepath.Join(localDir, "server.properties"), []byte("config1"), 0644)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(remoteDir, "server.properties"), []byte("config2"), 0644)
		assert.NoError(t, err)

		err = testhelpers.CompareInstanceDirectories(localDir, remoteDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})
}
