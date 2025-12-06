package testhelpers_test

import (
	"os"
	"path/filepath"
	"ritual/internal/testhelpers"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashDir(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		tempDir := t.TempDir()

		hash, err := testhelpers.HashDir(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("single file", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test.txt")

		err := os.WriteFile(testFile, []byte("hello world"), 0644)
		assert.NoError(t, err)

		hash, err := testhelpers.HashDir(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("multiple files", func(t *testing.T) {
		tempDir := t.TempDir()

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

		hash, err := testhelpers.HashDir(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("nested directories", func(t *testing.T) {
		tempDir := t.TempDir()

		subDir := filepath.Join(tempDir, "subdir")
		err := os.Mkdir(subDir, 0755)
		assert.NoError(t, err)

		nestedFile := filepath.Join(subDir, "nested.txt")
		err = os.WriteFile(nestedFile, []byte("nested content"), 0644)
		assert.NoError(t, err)

		rootFile := filepath.Join(tempDir, "root.txt")
		err = os.WriteFile(rootFile, []byte("root content"), 0644)
		assert.NoError(t, err)

		hash, err := testhelpers.HashDir(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("consistent hash for same content", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test.txt")

		err := os.WriteFile(testFile, []byte("same content"), 0644)
		assert.NoError(t, err)

		hash1, err := testhelpers.HashDir(tempDir)
		assert.NoError(t, err)

		hash2, err := testhelpers.HashDir(tempDir)
		assert.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("different hash for different content", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		err := os.WriteFile(filepath.Join(tempDir1, "file.txt"), []byte("content1"), 0644)
		assert.NoError(t, err)

		err = os.WriteFile(filepath.Join(tempDir2, "file.txt"), []byte("content2"), 0644)
		assert.NoError(t, err)

		hash1, err := testhelpers.HashDir(tempDir1)
		assert.NoError(t, err)

		hash2, err := testhelpers.HashDir(tempDir2)
		assert.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})
}

func TestHashDirs(t *testing.T) {
	t.Run("single directory", func(t *testing.T) {
		tempDir := t.TempDir()

		err := os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("content"), 0644)
		assert.NoError(t, err)

		hash, err := testhelpers.HashDirs(tempDir)
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("multiple directories", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		err := os.WriteFile(filepath.Join(tempDir1, "file1.txt"), []byte("content1"), 0644)
		assert.NoError(t, err)

		err = os.WriteFile(filepath.Join(tempDir2, "file2.txt"), []byte("content2"), 0644)
		assert.NoError(t, err)

		hash, err := testhelpers.HashDirs(tempDir1, tempDir2)
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
	})

	t.Run("consistent hash for same directories", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		err := os.WriteFile(filepath.Join(tempDir1, "file.txt"), []byte("content1"), 0644)
		assert.NoError(t, err)

		err = os.WriteFile(filepath.Join(tempDir2, "file.txt"), []byte("content2"), 0644)
		assert.NoError(t, err)

		hash1, err := testhelpers.HashDirs(tempDir1, tempDir2)
		assert.NoError(t, err)

		hash2, err := testhelpers.HashDirs(tempDir1, tempDir2)
		assert.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("order matters", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		err := os.WriteFile(filepath.Join(tempDir1, "file.txt"), []byte("content1"), 0644)
		assert.NoError(t, err)

		err = os.WriteFile(filepath.Join(tempDir2, "file.txt"), []byte("content2"), 0644)
		assert.NoError(t, err)

		hash1, err := testhelpers.HashDirs(tempDir1, tempDir2)
		assert.NoError(t, err)

		hash2, err := testhelpers.HashDirs(tempDir2, tempDir1)
		assert.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})
}

func TestCheckDirs(t *testing.T) {
	t.Run("identical directories match", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

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

		match, err := testhelpers.CheckDirs(testhelpers.DirPair{
			P1: []string{tempDir1},
			P2: []string{tempDir2},
		})
		assert.NoError(t, err)
		assert.True(t, match)
	})

	t.Run("different directories dont match", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		err := os.WriteFile(filepath.Join(tempDir1, "file.txt"), []byte("content1"), 0644)
		assert.NoError(t, err)

		err = os.WriteFile(filepath.Join(tempDir2, "file.txt"), []byte("content2"), 0644)
		assert.NoError(t, err)

		match, err := testhelpers.CheckDirs(testhelpers.DirPair{
			P1: []string{tempDir1},
			P2: []string{tempDir2},
		})
		assert.NoError(t, err)
		assert.False(t, match)
	})

	t.Run("multiple directories per side", func(t *testing.T) {
		tempDir1a := t.TempDir()
		tempDir1b := t.TempDir()
		tempDir2a := t.TempDir()
		tempDir2b := t.TempDir()

		// Create identical content across pairs
		err := os.WriteFile(filepath.Join(tempDir1a, "file.txt"), []byte("contentA"), 0644)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir2a, "file.txt"), []byte("contentA"), 0644)
		assert.NoError(t, err)

		err = os.WriteFile(filepath.Join(tempDir1b, "file.txt"), []byte("contentB"), 0644)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir2b, "file.txt"), []byte("contentB"), 0644)
		assert.NoError(t, err)

		match, err := testhelpers.CheckDirs(testhelpers.DirPair{
			P1: []string{tempDir1a, tempDir1b},
			P2: []string{tempDir2a, tempDir2b},
		})
		assert.NoError(t, err)
		assert.True(t, match)
	})

	t.Run("world directories comparison", func(t *testing.T) {
		localDir := t.TempDir()
		remoteDir := t.TempDir()

		worldDirs := []string{"world", "world_nether", "world_the_end"}

		var localPaths, remotePaths []string

		for _, worldDir := range worldDirs {
			localWorldDir := filepath.Join(localDir, worldDir)
			remoteWorldDir := filepath.Join(remoteDir, worldDir)

			err := os.Mkdir(localWorldDir, 0755)
			assert.NoError(t, err)
			err = os.Mkdir(remoteWorldDir, 0755)
			assert.NoError(t, err)

			err = os.WriteFile(filepath.Join(localWorldDir, "level.dat"), []byte("level data"), 0644)
			assert.NoError(t, err)
			err = os.WriteFile(filepath.Join(remoteWorldDir, "level.dat"), []byte("level data"), 0644)
			assert.NoError(t, err)

			localPaths = append(localPaths, localWorldDir)
			remotePaths = append(remotePaths, remoteWorldDir)
		}

		match, err := testhelpers.CheckDirs(testhelpers.DirPair{
			P1: localPaths,
			P2: remotePaths,
		})
		assert.NoError(t, err)
		assert.True(t, match)
	})
}
