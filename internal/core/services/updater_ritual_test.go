package services

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ritual/internal/core/ports/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamToFile_WritesBytesAndClosesHandle(t *testing.T) {
	payload := bytes.Repeat([]byte("RITUAL"), 10_000)

	body := &trackingReadCloser{Reader: bytes.NewReader(payload)}
	storage := &mocks.MockStorageRepository{
		GetStreamFunc: func(context.Context, string) (io.ReadCloser, error) {
			return body, nil
		},
	}

	dst := filepath.Join(t.TempDir(), "update.exe")
	n, err := streamToFile(t.Context(), storage, "remote/binary", dst)

	require.NoError(t, err, "streamToFile should succeed when the source stream is healthy")
	assert.Equal(t, int64(len(payload)), n,
		"streamToFile should report the exact number of bytes copied from the source stream")
	assert.True(t, body.closed,
		"streamToFile must close the source ReadCloser so the adapter can release its connection")

	written, err := os.ReadFile(dst) // #nosec G304 -- dst is t.TempDir()
	require.NoError(t, err, "destination file must be readable after streamToFile returns")
	assert.Equal(t, payload, written,
		"bytes on disk must match the source payload byte-for-byte")
}

func TestStreamToFile_GetStreamErrorSurfaces(t *testing.T) {
	storage := &mocks.MockStorageRepository{
		GetStreamFunc: func(context.Context, string) (io.ReadCloser, error) {
			return nil, io.ErrUnexpectedEOF
		},
	}

	dst := filepath.Join(t.TempDir(), "update.exe")
	_, err := streamToFile(t.Context(), storage, "remote/binary", dst)

	require.Error(t, err, "storage.GetStream failure must propagate out of streamToFile")
	assert.Contains(t, err.Error(), "failed to download",
		"streamToFile must wrap the underlying GetStream error with a 'failed to download' prefix")
	_, statErr := os.Stat(dst)
	assert.True(t, os.IsNotExist(statErr),
		"no destination file should be created when GetStream fails before a body is returned")
}

func TestStreamToFile_CopyErrorSurfacesAndClosesSource(t *testing.T) {
	body := &trackingReadCloser{Reader: &failingReader{err: io.ErrClosedPipe}}
	storage := &mocks.MockStorageRepository{
		GetStreamFunc: func(context.Context, string) (io.ReadCloser, error) {
			return body, nil
		},
	}

	dst := filepath.Join(t.TempDir(), "update.exe")
	_, err := streamToFile(t.Context(), storage, "remote/binary", dst)

	require.Error(t, err, "a failing source read must bubble up from streamToFile")
	assert.Contains(t, err.Error(), "failed to write update file",
		"copy errors must be wrapped with a 'failed to write update file' prefix so the caller knows the disk write aborted")
	assert.True(t, body.closed,
		"streamToFile must close the source ReadCloser even when the copy fails mid-stream")
}

func TestCopyFile_StreamsBytesBetweenPaths(t *testing.T) {
	src := filepath.Join(t.TempDir(), "current.exe")
	dst := filepath.Join(t.TempDir(), "old.exe")
	payload := []byte(strings.Repeat("A", 4096))
	require.NoError(t, os.WriteFile(src, payload, 0o644),
		"test setup must be able to seed the source file")

	require.NoError(t, copyFile(src, dst),
		"copyFile should succeed copying a readable source to a writable destination")

	got, err := os.ReadFile(dst) // #nosec G304 -- dst is t.TempDir()
	require.NoError(t, err, "destination file must be readable after copyFile returns")
	assert.Equal(t, payload, got,
		"copyFile must produce a byte-for-byte replica of the source file at the destination")
}

func TestCopyFile_MissingSourceErrors(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "old.exe")

	err := copyFile(filepath.Join(t.TempDir(), "nope.exe"), dst)

	require.Error(t, err, "copyFile must fail loudly when the source file does not exist")
	assert.Contains(t, err.Error(), "open ",
		"missing-source error must be wrapped with an 'open' prefix so the caller can locate the failure point")
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}

type failingReader struct{ err error }

func (f *failingReader) Read([]byte) (int, error) { return 0, f.err }
