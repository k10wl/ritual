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

// mockUploader implements S3StreamUploader for testing
type mockUploader struct {
	buf       *bytes.Buffer
	uploadErr error
}

func (m *mockUploader) Upload(ctx context.Context, bucket, key string, body io.Reader) (int64, error) {
	if m.uploadErr != nil {
		return 0, m.uploadErr
	}
	n, err := io.Copy(m.buf, body)
	return n, err
}

func TestPush_ValidatesInputs(t *testing.T) {
	uploader := &mockUploader{buf: &bytes.Buffer{}}

	t.Run("nil context returns error", func(t *testing.T) {
		cfg := PushConfig{Bucket: "b", Key: "k", Dirs: []string{"."}}
		_, err := Push(nil, cfg, uploader)
		assert.ErrorIs(t, err, ErrPushContextNil)
	})

	t.Run("empty bucket returns error", func(t *testing.T) {
		cfg := PushConfig{Bucket: "", Key: "k", Dirs: []string{"."}}
		_, err := Push(context.Background(), cfg, uploader)
		assert.ErrorIs(t, err, ErrPushBucketEmpty)
	})

	t.Run("empty key returns error", func(t *testing.T) {
		cfg := PushConfig{Bucket: "b", Key: "", Dirs: []string{"."}}
		_, err := Push(context.Background(), cfg, uploader)
		assert.ErrorIs(t, err, ErrPushKeyEmpty)
	})

	t.Run("empty dirs returns error", func(t *testing.T) {
		cfg := PushConfig{Bucket: "b", Key: "k", Dirs: []string{}}
		_, err := Push(context.Background(), cfg, uploader)
		assert.ErrorIs(t, err, ErrPushDirsEmpty)
	})

	t.Run("nil uploader returns error", func(t *testing.T) {
		cfg := PushConfig{Bucket: "b", Key: "k", Dirs: []string{"."}}
		_, err := Push(context.Background(), cfg, nil)
		assert.ErrorIs(t, err, ErrPushUploaderNil)
	})
}

func TestPush_CreatesValidTarGz(t *testing.T) {
	// Create test directory structure
	tempDir := t.TempDir()
	worldDir := filepath.Join(tempDir, "world")
	require.NoError(t, os.MkdirAll(worldDir, 0755))

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("level data"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(worldDir, "region"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "region", "r.0.0.mca"), []byte("region data"), 0644))

	// Run push
	buf := &bytes.Buffer{}
	uploader := &mockUploader{buf: buf}
	cfg := PushConfig{
		Bucket: "test-bucket",
		Key:    "backups/test.tar.gz",
		Dirs:   []string{worldDir},
	}

	result, err := Push(context.Background(), cfg, uploader)
	require.NoError(t, err)

	// Verify result
	assert.Equal(t, "backups/test.tar.gz", result.Key)
	assert.Greater(t, result.Size, int64(0))
	assert.NotEmpty(t, result.Checksum)
	assert.Len(t, result.Checksum, 64) // SHA-256 hex

	// Verify archive contents
	gzReader, err := gzip.NewReader(buf)
	require.NoError(t, err)
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	files := make(map[string][]byte)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tarReader)
			require.NoError(t, err)
			files[header.Name] = data
		}
	}

	assert.Contains(t, files, "world/level.dat")
	assert.Contains(t, files, "world/region/r.0.0.mca")
	assert.Equal(t, []byte("level data"), files["world/level.dat"])
	assert.Equal(t, []byte("region data"), files["world/region/r.0.0.mca"])
}

func TestPush_MultipleDirs(t *testing.T) {
	tempDir := t.TempDir()

	// Create multiple world directories
	for _, name := range []string{"world", "world_nether", "world_the_end"} {
		dir := filepath.Join(tempDir, name)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "level.dat"), []byte(name), 0644))
	}

	buf := &bytes.Buffer{}
	uploader := &mockUploader{buf: buf}
	cfg := PushConfig{
		Bucket: "test-bucket",
		Key:    "test.tar.gz",
		Dirs: []string{
			filepath.Join(tempDir, "world"),
			filepath.Join(tempDir, "world_nether"),
			filepath.Join(tempDir, "world_the_end"),
		},
	}

	_, err := Push(context.Background(), cfg, uploader)
	require.NoError(t, err)

	// Verify all directories are in archive
	gzReader, err := gzip.NewReader(buf)
	require.NoError(t, err)
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	foundDirs := make(map[string]bool)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		parts := filepath.SplitList(header.Name)
		if len(parts) > 0 {
			foundDirs[filepath.Dir(header.Name)] = true
		}
	}

	assert.True(t, foundDirs["world"] || foundDirs["world/"])
	assert.True(t, foundDirs["world_nether"] || foundDirs["world_nether/"])
	assert.True(t, foundDirs["world_the_end"] || foundDirs["world_the_end/"])
}

func TestPush_LocalBackup(t *testing.T) {
	tempDir := t.TempDir()
	worldDir := filepath.Join(tempDir, "world")
	require.NoError(t, os.MkdirAll(worldDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("data"), 0644))

	t.Run("writes local copy when condition true", func(t *testing.T) {
		localPath := filepath.Join(tempDir, "backup", "test.tar.gz")
		buf := &bytes.Buffer{}
		uploader := &mockUploader{buf: buf}
		cfg := PushConfig{
			Bucket:       "test-bucket",
			Key:          "test.tar.gz",
			Dirs:         []string{worldDir},
			LocalPath:    localPath,
			ShouldBackup: func() bool { return true },
		}

		result, err := Push(context.Background(), cfg, uploader)
		require.NoError(t, err)
		assert.Equal(t, localPath, result.LocalPath)

		// Verify local file exists and matches
		localData, err := os.ReadFile(localPath)
		require.NoError(t, err)
		assert.Equal(t, buf.Bytes(), localData)
	})

	t.Run("skips local copy when condition false", func(t *testing.T) {
		localPath := filepath.Join(tempDir, "backup2", "test.tar.gz")
		buf := &bytes.Buffer{}
		uploader := &mockUploader{buf: buf}
		cfg := PushConfig{
			Bucket:       "test-bucket",
			Key:          "test.tar.gz",
			Dirs:         []string{worldDir},
			LocalPath:    localPath,
			ShouldBackup: func() bool { return false },
		}

		result, err := Push(context.Background(), cfg, uploader)
		require.NoError(t, err)
		assert.Empty(t, result.LocalPath)

		// Verify local file does not exist
		_, err = os.Stat(localPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("writes local copy when ShouldBackup is nil", func(t *testing.T) {
		localPath := filepath.Join(tempDir, "backup3", "test.tar.gz")
		buf := &bytes.Buffer{}
		uploader := &mockUploader{buf: buf}
		cfg := PushConfig{
			Bucket:       "test-bucket",
			Key:          "test.tar.gz",
			Dirs:         []string{worldDir},
			LocalPath:    localPath,
			ShouldBackup: nil, // nil means always backup
		}

		result, err := Push(context.Background(), cfg, uploader)
		require.NoError(t, err)
		assert.Equal(t, localPath, result.LocalPath)
	})

	t.Run("skips local copy when path empty", func(t *testing.T) {
		buf := &bytes.Buffer{}
		uploader := &mockUploader{buf: buf}
		cfg := PushConfig{
			Bucket:       "test-bucket",
			Key:          "test.tar.gz",
			Dirs:         []string{worldDir},
			LocalPath:    "",
			ShouldBackup: func() bool { return true },
		}

		result, err := Push(context.Background(), cfg, uploader)
		require.NoError(t, err)
		assert.Empty(t, result.LocalPath)
	})
}

func TestPush_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	worldDir := filepath.Join(tempDir, "world")
	require.NoError(t, os.MkdirAll(worldDir, 0755))

	// Create some files
	for i := 0; i < 10; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(worldDir, filepath.Join("file"+string(rune('0'+i))+".dat")),
			make([]byte, 1024),
			0644,
		))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	buf := &bytes.Buffer{}
	uploader := &mockUploader{buf: buf}
	cfg := PushConfig{
		Bucket: "test-bucket",
		Key:    "test.tar.gz",
		Dirs:   []string{worldDir},
	}

	_, err := Push(ctx, cfg, uploader)
	assert.Error(t, err)
}

func TestPush_UploadFailure(t *testing.T) {
	tempDir := t.TempDir()
	worldDir := filepath.Join(tempDir, "world")
	require.NoError(t, os.MkdirAll(worldDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("data"), 0644))

	localPath := filepath.Join(tempDir, "backup", "test.tar.gz")
	uploader := &mockUploader{
		buf:       &bytes.Buffer{},
		uploadErr: assert.AnError,
	}
	cfg := PushConfig{
		Bucket:       "test-bucket",
		Key:          "test.tar.gz",
		Dirs:         []string{worldDir},
		LocalPath:    localPath,
		ShouldBackup: func() bool { return true },
	}

	_, err := Push(context.Background(), cfg, uploader)
	assert.Error(t, err)

	// Verify local file was cleaned up
	_, statErr := os.Stat(localPath)
	assert.True(t, os.IsNotExist(statErr))
}

func TestPush_Checksum(t *testing.T) {
	tempDir := t.TempDir()
	worldDir := filepath.Join(tempDir, "world")
	require.NoError(t, os.MkdirAll(worldDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("data"), 0644))

	buf := &bytes.Buffer{}
	uploader := &mockUploader{buf: buf}
	cfg := PushConfig{
		Bucket: "test-bucket",
		Key:    "test.tar.gz",
		Dirs:   []string{worldDir},
	}

	// Run twice and verify checksum is consistent
	result1, err := Push(context.Background(), cfg, uploader)
	require.NoError(t, err)

	buf.Reset()
	result2, err := Push(context.Background(), cfg, uploader)
	require.NoError(t, err)

	assert.Equal(t, result1.Checksum, result2.Checksum)
}

func TestAddDirToTar_PreservesStructure(t *testing.T) {
	tempDir := t.TempDir()
	worldDir := filepath.Join(tempDir, "myworld")
	require.NoError(t, os.MkdirAll(filepath.Join(worldDir, "region"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(worldDir, "data"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "level.dat"), []byte("level"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "region", "r.0.0.mca"), []byte("region"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "data", "map.dat"), []byte("map"), 0644))

	buf := &bytes.Buffer{}
	gzWriter := gzip.NewWriter(buf)
	tarWriter := tar.NewWriter(gzWriter)

	err := addDirToTar(context.Background(), tarWriter, worldDir)
	require.NoError(t, err)

	tarWriter.Close()
	gzWriter.Close()

	// Read back and verify
	gzReader, err := gzip.NewReader(buf)
	require.NoError(t, err)
	tarReader := tar.NewReader(gzReader)

	entries := make(map[string]bool)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		entries[header.Name] = true
	}

	assert.True(t, entries["myworld/"] || entries["myworld"])
	assert.True(t, entries["myworld/level.dat"])
	assert.True(t, entries["myworld/region/r.0.0.mca"])
	assert.True(t, entries["myworld/data/map.dat"])
}
