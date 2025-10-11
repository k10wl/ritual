package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestFSRepository_Get(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("successful get", func(t *testing.T) {
		key := "test/key"
		expectedData := []byte("test data")

		err := repo.Put(key, expectedData)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		data, err := repo.Get(key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if string(data) != string(expectedData) {
			t.Errorf("Expected %s, got %s", string(expectedData), string(data))
		}
	})

	t.Run("key not found", func(t *testing.T) {
		_, err := repo.Get("nonexistent/key")
		if err == nil {
			t.Error("Expected error for nonexistent key")
		}
	})
}

func TestFSRepository_Put(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("successful put", func(t *testing.T) {
		key := "test/key"
		data := []byte("test data")

		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		path := filepath.Join(tempDir, key)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("File was not created")
		}
	})

	t.Run("creates directories", func(t *testing.T) {
		key := "deep/nested/path/key"
		data := []byte("test data")

		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		path := filepath.Join(tempDir, key)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("File was not created in nested directory")
		}
	})
}

func TestFSRepository_Delete(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("successful delete", func(t *testing.T) {
		key := "test/key"
		data := []byte("test data")

		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err = repo.Delete(key)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		path := filepath.Join(tempDir, key)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("File was not deleted")
		}
	})

	t.Run("delete nonexistent key", func(t *testing.T) {
		err := repo.Delete("nonexistent/key")
		if err == nil {
			t.Error("Expected error for deleting nonexistent key")
		}
	})
}

func TestFSRepository_List(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("list with prefix", func(t *testing.T) {
		keys := []string{
			"prefix/key1",
			"prefix/key2",
			"other/key3",
		}

		for _, key := range keys {
			err := repo.Put(key, []byte("data"))
			if err != nil {
				t.Fatalf("Put failed for %s: %v", key, err)
			}
		}

		result, err := repo.List("prefix")
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(result) != 2 {
			t.Errorf("Expected 2 keys, got %d", len(result))
		}
	})

	t.Run("list empty prefix", func(t *testing.T) {
		result, err := repo.List("nonexistent")
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(result) != 0 {
			t.Errorf("Expected 0 keys, got %d", len(result))
		}
	})
}

func TestFSRepository_ManifestOperations(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("store and retrieve manifest", func(t *testing.T) {
		key := "manifests/test.json"
		data := []byte(`{"version":"1.0.0","instance_id":"test-instance","updated_at":"2023-01-01T00:00:00Z"}`)

		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Put manifest failed: %v", err)
		}

		retrievedData, err := repo.Get(key)
		if err != nil {
			t.Fatalf("Get manifest failed: %v", err)
		}

		if string(retrievedData) != string(data) {
			t.Errorf("Manifest data mismatch")
		}
	})
}

func TestFSRepository_ErrorConditions(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("permission denied", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Permission testing on Windows requires elevated privileges")
		}

		readOnlyDir := filepath.Join(tempDir, "readonly")
		err := os.Mkdir(readOnlyDir, 0400)
		if err != nil {
			t.Fatalf("Failed to create read-only directory: %v", err)
		}
		defer os.Chmod(readOnlyDir, 0755)

		readOnlyRepo, err := NewFSRepository(readOnlyDir)
		if err != nil {
			t.Fatalf("Failed to create read-only repository: %v", err)
		}
		defer readOnlyRepo.Close()
		err = readOnlyRepo.Put("test", []byte("data"))
		if err == nil {
			t.Error("Expected permission error")
		}
	})

	t.Run("invalid empty key", func(t *testing.T) {
		err := repo.Put("", []byte("data"))
		if err == nil {
			t.Error("Expected error for empty key")
		}
	})

	t.Run("path traversal attempt", func(t *testing.T) {
		err := repo.Put("../outside", []byte("data"))
		if err == nil {
			t.Error("Expected error for path traversal attempt")
		}
	})

	t.Run("null byte in key", func(t *testing.T) {
		err := repo.Put("test\x00key", []byte("data"))
		if err == nil {
			t.Error("Expected error for null byte in key")
		}
	})
}

func TestFSRepository_BoundaryValues(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("empty data", func(t *testing.T) {
		key := "empty"
		err := repo.Put(key, []byte{})
		if err != nil {
			t.Fatalf("Put empty data failed: %v", err)
		}

		data, err := repo.Get(key)
		if err != nil {
			t.Fatalf("Get empty data failed: %v", err)
		}

		if len(data) != 0 {
			t.Errorf("Expected empty data, got %d bytes", len(data))
		}
	})

	t.Run("unicode key", func(t *testing.T) {
		key := "测试/unicode/ключ"
		data := []byte("unicode test")
		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Put unicode key failed: %v", err)
		}

		retrievedData, err := repo.Get(key)
		if err != nil {
			t.Fatalf("Get unicode key failed: %v", err)
		}

		if string(retrievedData) != string(data) {
			t.Errorf("Unicode key data mismatch")
		}
	})

	t.Run("very long key", func(t *testing.T) {
		longKey := "long/" + strings.Repeat("a", 200)

		data := []byte("long key test")
		err := repo.Put(longKey, data)
		if err != nil {
			t.Fatalf("Put long key failed: %v", err)
		}

		retrievedData, err := repo.Get(longKey)
		if err != nil {
			t.Fatalf("Get long key failed: %v", err)
		}

		if string(retrievedData) != string(data) {
			t.Errorf("Long key data mismatch")
		}
	})
}

func TestFSRepository_Concurrency(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("concurrent reads", func(t *testing.T) {
		key := "concurrent"
		data := []byte("concurrent test data")
		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}

		var wg sync.WaitGroup
		numGoroutines := 100
		errors := make(chan error, numGoroutines)

		for range numGoroutines {
			wg.Go(func() {
				retrievedData, err := repo.Get(key)
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
				err := repo.Put(key, data)
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
		err := repo.Put(key, initialData)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}

		var wg sync.WaitGroup
		errors := make(chan error, 2)

		wg.Go(func() {
			for i := range 100 {
				data := []byte("race-data-" + string(rune(i)))
				err := repo.Put(key, data)
				if err != nil {
					errors <- err
					return
				}
			}
		})

		wg.Go(func() {
			for range 100 {
				_, err := repo.Get(key)
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
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("path traversal prevention", func(t *testing.T) {
		maliciousKeys := []string{
			"../../../etc/passwd",
			"..\\..\\windows\\system32",
			"../manifest.json",
			"..\\config.json",
		}

		for _, key := range maliciousKeys {
			err := repo.Put(key, []byte("malicious"))
			if err == nil {
				t.Errorf("Path traversal not prevented for key: %s", key)
			}
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
			err := repo.Put(name, []byte("test"))
			if err == nil {
				t.Logf("Reserved name not prevented: %s", name)
			}
		}
	})
}

func TestFSRepository_SpacesInDirectoryNames(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := NewFSRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	defer repo.Close()

	t.Run("spaces in directory names", func(t *testing.T) {
		key := "directory with spaces/file with spaces.txt"
		data := []byte("file with spaces content")

		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Put with spaces failed: %v", err)
		}

		retrievedData, err := repo.Get(key)
		if err != nil {
			t.Fatalf("Get with spaces failed: %v", err)
		}

		if string(retrievedData) != string(data) {
			t.Errorf("Spaces data mismatch")
		}
	})

	t.Run("multiple spaces in path", func(t *testing.T) {
		key := "multiple spaces/nested/file.txt"
		data := []byte("multiple spaces content")

		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Put multiple spaces failed: %v", err)
		}

		retrievedData, err := repo.Get(key)
		if err != nil {
			t.Fatalf("Get multiple spaces failed: %v", err)
		}

		if string(retrievedData) != string(data) {
			t.Errorf("Multiple spaces data mismatch")
		}
	})

	t.Run("leading and trailing spaces", func(t *testing.T) {
		key := " leading spaces/file.txt"
		data := []byte("leading spaces content")

		err := repo.Put(key, data)
		if err != nil {
			t.Fatalf("Put leading spaces failed: %v", err)
		}

		retrievedData, err := repo.Get(key)
		if err != nil {
			t.Fatalf("Get leading spaces failed: %v", err)
		}

		if string(retrievedData) != string(data) {
			t.Errorf("Leading spaces data mismatch")
		}
	})
}
