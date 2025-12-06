package streamer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDownloader implements S3StreamDownloader for testing
type mockDownloader struct {
	data        []byte
	downloadErr error
}

func (m *mockDownloader) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

// createTestArchive creates a tar.gz archive for testing
func createTestArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	buf := &bytes.Buffer{}
	gzWriter := gzip.NewWriter(buf)
	tarWriter := tar.NewWriter(gzWriter)

	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if content == nil {
			// Directory
			header.Typeflag = tar.TypeDir
			header.Mode = 0755
			header.Size = 0
		} else {
			header.Typeflag = tar.TypeReg
		}

		require.NoError(t, tarWriter.WriteHeader(header))
		if content != nil {
			_, err := tarWriter.Write(content)
			require.NoError(t, err)
		}
	}

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzWriter.Close())

	return buf.Bytes()
}

func TestPull_ValidatesInputs(t *testing.T) {
	downloader := &mockDownloader{data: []byte{}}

	t.Run("nil context returns error", func(t *testing.T) {
		cfg := PullConfig{Bucket: "b", Key: "k", Dest: "."}
		err := Pull(nil, cfg, downloader)
		assert.ErrorIs(t, err, ErrPullContextNil)
	})

	t.Run("empty bucket returns error", func(t *testing.T) {
		cfg := PullConfig{Bucket: "", Key: "k", Dest: "."}
		err := Pull(context.Background(), cfg, downloader)
		assert.ErrorIs(t, err, ErrPullBucketEmpty)
	})

	t.Run("empty key returns error", func(t *testing.T) {
		cfg := PullConfig{Bucket: "b", Key: "", Dest: "."}
		err := Pull(context.Background(), cfg, downloader)
		assert.ErrorIs(t, err, ErrPullKeyEmpty)
	})

	t.Run("empty dest returns error", func(t *testing.T) {
		cfg := PullConfig{Bucket: "b", Key: "k", Dest: ""}
		err := Pull(context.Background(), cfg, downloader)
		assert.ErrorIs(t, err, ErrPullDestEmpty)
	})

	t.Run("nil downloader returns error", func(t *testing.T) {
		cfg := PullConfig{Bucket: "b", Key: "k", Dest: "."}
		err := Pull(context.Background(), cfg, nil)
		assert.ErrorIs(t, err, ErrPullDownloaderNil)
	})
}

func TestPull_ExtractsFiles(t *testing.T) {
	tempDir := t.TempDir()

	archive := createTestArchive(t, map[string][]byte{
		"world/":                nil,
		"world/level.dat":       []byte("level data"),
		"world/region/":         nil,
		"world/region/r.0.0.mca": []byte("region data"),
	})

	downloader := &mockDownloader{data: archive}
	cfg := PullConfig{
		Bucket:   "test-bucket",
		Key:      "test.tar.gz",
		Dest:     tempDir,
		Conflict: Replace,
	}

	err := Pull(context.Background(), cfg, downloader)
	require.NoError(t, err)

	// Verify extracted files
	levelData, err := os.ReadFile(filepath.Join(tempDir, "world", "level.dat"))
	require.NoError(t, err)
	assert.Equal(t, []byte("level data"), levelData)

	regionData, err := os.ReadFile(filepath.Join(tempDir, "world", "region", "r.0.0.mca"))
	require.NoError(t, err)
	assert.Equal(t, []byte("region data"), regionData)
}

func TestPull_PathTraversal(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("rejects ../escape paths", func(t *testing.T) {
		archive := createTestArchive(t, map[string][]byte{
			"../escape.txt": []byte("malicious"),
		})

		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket: "test-bucket",
			Key:    "test.tar.gz",
			Dest:   tempDir,
		}

		err := Pull(context.Background(), cfg, downloader)
		assert.ErrorIs(t, err, ErrPathTraversal)
	})

	t.Run("rejects nested ../escape paths", func(t *testing.T) {
		archive := createTestArchive(t, map[string][]byte{
			"world/../../escape.txt": []byte("malicious"),
		})

		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket: "test-bucket",
			Key:    "test.tar.gz",
			Dest:   tempDir,
		}

		err := Pull(context.Background(), cfg, downloader)
		assert.ErrorIs(t, err, ErrPathTraversal)
	})

	t.Run("accepts valid relative paths", func(t *testing.T) {
		archive := createTestArchive(t, map[string][]byte{
			"world/level.dat": []byte("valid"),
		})

		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket: "test-bucket",
			Key:    "test.tar.gz",
			Dest:   tempDir,
		}

		err := Pull(context.Background(), cfg, downloader)
		assert.NoError(t, err)
	})
}

func TestPull_ConflictStrategies(t *testing.T) {
	t.Run("Replace overwrites existing", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create existing file
		worldDir := filepath.Join(tempDir, "world")
		require.NoError(t, os.MkdirAll(worldDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("old"), 0644))

		archive := createTestArchive(t, map[string][]byte{
			"world/level.dat": []byte("new"),
		})

		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket:   "test-bucket",
			Key:      "test.tar.gz",
			Dest:     tempDir,
			Conflict: Replace,
		}

		err := Pull(context.Background(), cfg, downloader)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(worldDir, "level.dat"))
		require.NoError(t, err)
		assert.Equal(t, []byte("new"), data)
	})

	t.Run("Skip preserves existing", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create existing file
		worldDir := filepath.Join(tempDir, "world")
		require.NoError(t, os.MkdirAll(worldDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("old"), 0644))

		archive := createTestArchive(t, map[string][]byte{
			"world/level.dat": []byte("new"),
		})

		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket:   "test-bucket",
			Key:      "test.tar.gz",
			Dest:     tempDir,
			Conflict: Skip,
		}

		err := Pull(context.Background(), cfg, downloader)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(worldDir, "level.dat"))
		require.NoError(t, err)
		assert.Equal(t, []byte("old"), data)
	})

	t.Run("Backup creates .bak", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create existing file
		worldDir := filepath.Join(tempDir, "world")
		require.NoError(t, os.MkdirAll(worldDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("old"), 0644))

		archive := createTestArchive(t, map[string][]byte{
			"world/level.dat": []byte("new"),
		})

		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket:   "test-bucket",
			Key:      "test.tar.gz",
			Dest:     tempDir,
			Conflict: Backup,
		}

		err := Pull(context.Background(), cfg, downloader)
		require.NoError(t, err)

		// Verify new file
		data, err := os.ReadFile(filepath.Join(worldDir, "level.dat"))
		require.NoError(t, err)
		assert.Equal(t, []byte("new"), data)

		// Verify backup
		bakData, err := os.ReadFile(filepath.Join(worldDir, "level.dat.bak"))
		require.NoError(t, err)
		assert.Equal(t, []byte("old"), bakData)
	})

	t.Run("Fail returns error on conflict", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create existing file
		worldDir := filepath.Join(tempDir, "world")
		require.NoError(t, os.MkdirAll(worldDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("old"), 0644))

		archive := createTestArchive(t, map[string][]byte{
			"world/level.dat": []byte("new"),
		})

		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket:   "test-bucket",
			Key:      "test.tar.gz",
			Dest:     tempDir,
			Conflict: Fail,
		}

		err := Pull(context.Background(), cfg, downloader)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file already exists")
	})
}

func TestPull_Filter(t *testing.T) {
	tempDir := t.TempDir()

	archive := createTestArchive(t, map[string][]byte{
		"world/level.dat": []byte("level"),
		"world/session.lock": []byte("lock"),
		"world/region/r.0.0.mca": []byte("region"),
	})

	t.Run("extracts only matching files", func(t *testing.T) {
		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket:   "test-bucket",
			Key:      "test.tar.gz",
			Dest:     tempDir,
			Conflict: Replace,
			Filter: func(name string) bool {
				// Only extract .dat files
				return filepath.Ext(name) == ".dat"
			},
		}

		err := Pull(context.Background(), cfg, downloader)
		require.NoError(t, err)

		// level.dat should exist
		_, err = os.Stat(filepath.Join(tempDir, "world", "level.dat"))
		assert.NoError(t, err)

		// session.lock should NOT exist
		_, err = os.Stat(filepath.Join(tempDir, "world", "session.lock"))
		assert.True(t, os.IsNotExist(err))

		// r.0.0.mca should NOT exist
		_, err = os.Stat(filepath.Join(tempDir, "world", "region", "r.0.0.mca"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("nil filter extracts all", func(t *testing.T) {
		destDir := filepath.Join(tempDir, "all")
		downloader := &mockDownloader{data: archive}
		cfg := PullConfig{
			Bucket:   "test-bucket",
			Key:      "test.tar.gz",
			Dest:     destDir,
			Conflict: Replace,
			Filter:   nil,
		}

		err := Pull(context.Background(), cfg, downloader)
		require.NoError(t, err)

		// All files should exist
		_, err = os.Stat(filepath.Join(destDir, "world", "level.dat"))
		assert.NoError(t, err)

		_, err = os.Stat(filepath.Join(destDir, "world", "session.lock"))
		assert.NoError(t, err)

		_, err = os.Stat(filepath.Join(destDir, "world", "region", "r.0.0.mca"))
		assert.NoError(t, err)
	})
}

func TestPull_ContextCancellation(t *testing.T) {
	// Create archive with many files
	files := make(map[string][]byte)
	for i := 0; i < 100; i++ {
		files["world/file"+string(rune('0'+i%10))+".dat"] = make([]byte, 1024)
	}
	archive := createTestArchive(t, files)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	tempDir := t.TempDir()
	downloader := &mockDownloader{data: archive}
	cfg := PullConfig{
		Bucket: "test-bucket",
		Key:    "test.tar.gz",
		Dest:   tempDir,
	}

	err := Pull(ctx, cfg, downloader)
	assert.Error(t, err)
}

func TestPull_DownloadFailure(t *testing.T) {
	tempDir := t.TempDir()
	downloader := &mockDownloader{downloadErr: assert.AnError}
	cfg := PullConfig{
		Bucket: "test-bucket",
		Key:    "test.tar.gz",
		Dest:   tempDir,
	}

	err := Pull(context.Background(), cfg, downloader)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "R2 download failed")
}

func TestIsPathSafe(t *testing.T) {
	tests := []struct {
		name       string
		baseDir    string
		targetPath string
		expected   bool
	}{
		{"valid relative path", "/base", "/base/file.txt", true},
		{"valid nested path", "/base", "/base/dir/file.txt", true},
		{"escape with ..", "/base", "/base/../escape.txt", false},
		{"nested escape", "/base", "/base/dir/../../escape.txt", false},
		{"absolute escape", "/base", "/other/file.txt", false},
		{"empty base", "", "/base/file.txt", false},
		{"empty target", "/base", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathSafe(tt.baseDir, tt.targetPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}
