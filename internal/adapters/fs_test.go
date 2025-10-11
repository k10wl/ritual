package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFSRepository_Get(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("successful get", func(t *testing.T) {
		key := "test/key"
		expectedData := []byte("test data")

		err := repo.Put(ctx, key, expectedData)
		assert.NoError(t, err)

		data, err := repo.Get(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, string(expectedData), string(data))
	})

	t.Run("key not found", func(t *testing.T) {
		_, err := repo.Get(ctx, "nonexistent/key")
		assert.Error(t, err, "Expected error for nonexistent key")
	})
}

func TestFSRepository_Put(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("successful put", func(t *testing.T) {
		key := "test/key"
		data := []byte("test data")

		err := repo.Put(ctx, key, data)
		assert.NoError(t, err)

		path := filepath.Join(tempDir, key)
		_, err = os.Stat(path)
		assert.NoError(t, err, "File was not created")
	})

	t.Run("creates directories", func(t *testing.T) {
		key := "deep/nested/path/key"
		data := []byte("test data")

		err := repo.Put(ctx, key, data)
		assert.NoError(t, err)

		path := filepath.Join(tempDir, key)
		_, err = os.Stat(path)
		assert.NoError(t, err, "File was not created in nested directory")
	})
}

func TestFSRepository_Delete(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("successful delete", func(t *testing.T) {
		key := "test/key"
		data := []byte("test data")

		err := repo.Put(ctx, key, data)
		assert.NoError(t, err)

		err = repo.Delete(ctx, key)
		assert.NoError(t, err)

		path := filepath.Join(tempDir, key)
		_, err = os.Stat(path)
		assert.True(t, os.IsNotExist(err), "File was not deleted")
	})

	t.Run("delete nonexistent key", func(t *testing.T) {
		err := repo.Delete(ctx, "nonexistent/key")
		assert.Error(t, err, "Expected error for deleting nonexistent key")
	})
}

func TestFSRepository_List(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("list with prefix", func(t *testing.T) {
		keys := []string{
			"prefix/key1",
			"prefix/key2",
			"other/key3",
		}

		for _, key := range keys {
			err := repo.Put(ctx, key, []byte("data"))
			assert.NoError(t, err)
		}

		result, err := repo.List(ctx, "prefix")
		assert.NoError(t, err)
		assert.Len(t, result, 2, "Expected 2 keys")
	})

	t.Run("list empty prefix", func(t *testing.T) {
		result, err := repo.List(ctx, "nonexistent")
		assert.NoError(t, err)
		assert.Len(t, result, 0, "Expected 0 keys")
	})
}

func TestFSRepository_ManifestOperations(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("store and retrieve manifest", func(t *testing.T) {
		key := "manifests/test.json"
		data := []byte(`{"version":"1.0.0","instance_id":"test-instance","updated_at":"2023-01-01T00:00:00Z"}`)

		err := repo.Put(ctx, key, data)
		assert.NoError(t, err)

		retrievedData, err := repo.Get(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, string(data), string(retrievedData), "Manifest data mismatch")
	})
}

func TestFSRepository_ErrorConditions(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("permission denied", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Permission testing on Windows requires elevated privileges")
		}

		readOnlyDir := filepath.Join(tempDir, "readonly")
		err := os.Mkdir(readOnlyDir, 0400)
		assert.NoError(t, err)
		defer os.Chmod(readOnlyDir, 0755)

		readOnlyRepo, err := NewFSRepository(readOnlyDir)
		assert.NoError(t, err)
		defer readOnlyRepo.Close()

		err = readOnlyRepo.Put(ctx, "test", []byte("data"))
		assert.Error(t, err, "Expected permission error")
	})

	t.Run("invalid empty key", func(t *testing.T) {
		err := repo.Put(ctx, "", []byte("data"))
		assert.Error(t, err, "Expected error for empty key")
	})

	t.Run("path traversal attempt", func(t *testing.T) {
		err := repo.Put(ctx, "../outside", []byte("data"))
		assert.Error(t, err, "Expected error for path traversal attempt")
	})

	t.Run("null byte in key", func(t *testing.T) {
		err := repo.Put(ctx, "test\x00key", []byte("data"))
		assert.Error(t, err, "Expected error for null byte in key")
	})
}

func TestFSRepository_BoundaryValues(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("empty data", func(t *testing.T) {
		key := "empty"
		err := repo.Put(ctx, key, []byte{})
		assert.NoError(t, err)

		data, err := repo.Get(ctx, key)
		assert.NoError(t, err)
		assert.Len(t, data, 0, "Expected empty data")
	})

	t.Run("unicode key", func(t *testing.T) {
		key := "测试/unicode/ключ"
		data := []byte("unicode test")
		err := repo.Put(ctx, key, data)
		assert.NoError(t, err)

		retrievedData, err := repo.Get(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, string(data), string(retrievedData), "Unicode key data mismatch")
	})

	t.Run("very long key", func(t *testing.T) {
		longKey := "long/" + strings.Repeat("a", 200)

		data := []byte("long key test")
		err := repo.Put(context.Background(), longKey, data)
		assert.NoError(t, err)

		retrievedData, err := repo.Get(context.Background(), longKey)
		assert.NoError(t, err)
		assert.Equal(t, string(data), string(retrievedData), "Long key data mismatch")
	})
}

func TestFSRepository_Concurrency(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("concurrent reads", func(t *testing.T) {
		key := "concurrent"
		data := []byte("concurrent test data")
		err := repo.Put(ctx, key, data)
		assert.NoError(t, err)

		var wg sync.WaitGroup
		numGoroutines := 100
		errors := make(chan error, numGoroutines)

		for range numGoroutines {
			wg.Go(func() {
				retrievedData, err := repo.Get(ctx, key)
				if err != nil {
					errors <- err
					return
				}
				if string(retrievedData) != string(data) {
					errors <- err
				}
			})
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("Concurrent read error: %v", err)
		}
	})

	t.Run("concurrent writes", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50
		errors := make(chan error, numGoroutines)

		for i := range numGoroutines {
			wg.Go(func() {
				key := fmt.Sprintf("concurrent-write-%d", i)
				data := []byte(fmt.Sprintf("data-%d", i))
				err := repo.Put(ctx, key, data)
				if err != nil {
					errors <- err
				}
			})
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("Concurrent write error: %v", err)
		}
	})

	t.Run("read write race", func(t *testing.T) {
		key := "race"
		initialData := []byte("initial")
		err := repo.Put(context.Background(), key, initialData)
		assert.NoError(t, err)

		var wg sync.WaitGroup
		errors := make(chan error, 2)

		wg.Go(func() {
			for i := range 100 {
				data := []byte("race-data-" + string(rune(i)))
				err := repo.Put(ctx, key, data)
				if err != nil {
					errors <- err
					return
				}
			}
		})

		wg.Go(func() {
			for range 100 {
				_, err := repo.Get(ctx, key)
				if err != nil {
					errors <- err
					return
				}
			}
		})

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("Race condition error: %v", err)
		}
	})
}

func TestFSRepository_Security(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("path traversal prevention", func(t *testing.T) {
		maliciousKeys := []string{
			"../../../etc/passwd",
			"..\\..\\windows\\system32",
			"../manifest.json",
			"..\\config.json",
		}

		for _, key := range maliciousKeys {
			err := repo.Put(ctx, key, []byte("malicious"))
			assert.Error(t, err, "Path traversal not prevented for key: %s", key)
		}
	})

	t.Run("windows reserved names", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Windows reserved names test only on Windows")
		}

		reservedNames := []string{
			"CON",
			"PRN",
			"AUX",
			"NUL",
			"COM1",
			"LPT1",
		}

		for _, name := range reservedNames {
			err := repo.Put(ctx, name, []byte("test"))
			if err == nil {
				t.Logf("Reserved name not prevented: %s", name)
			}
		}
	})
}

func TestFSRepository_SpacesInDirectoryNames(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	assert.NoError(t, err)
	defer repo.Close()

	t.Run("spaces in directory names", func(t *testing.T) {
		key := "directory with spaces/file with spaces.txt"
		data := []byte("file with spaces content")

		err := repo.Put(context.Background(), key, data)
		assert.NoError(t, err)

		retrievedData, err := repo.Get(context.Background(), key)
		assert.NoError(t, err)
		assert.Equal(t, string(data), string(retrievedData), "Spaces data mismatch")
	})

	t.Run("multiple spaces in path", func(t *testing.T) {
		key := "multiple spaces/nested/file.txt"
		data := []byte("multiple spaces content")

		err := repo.Put(context.Background(), key, data)
		assert.NoError(t, err)

		retrievedData, err := repo.Get(context.Background(), key)
		assert.NoError(t, err)
		assert.Equal(t, string(data), string(retrievedData), "Multiple spaces data mismatch")
	})

	t.Run("leading and trailing spaces", func(t *testing.T) {
		key := " leading spaces/file.txt"
		data := []byte("leading spaces content")

		err := repo.Put(context.Background(), key, data)
		assert.NoError(t, err)

		retrievedData, err := repo.Get(context.Background(), key)
		assert.NoError(t, err)
		assert.Equal(t, string(data), string(retrievedData), "Leading spaces data mismatch")
	})
}
