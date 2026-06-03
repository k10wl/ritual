package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
)

// These exercise the REAL github.com/minio/selfupdate.Apply — atomic replace +
// checksum verify + rollback — against a temp TargetPath instead of the test
// binary. Closes design-log/037 verification criterion #2 (mismatched sha must
// not replace the file) with the actual library, not the mocked applyFn.
//
// The Updater drives the body + decoded checksum; the test only redirects the
// replace target to a temp file via Options.TargetPath.

func realApplyTo(target string) func(io.Reader, selfupdate.Options) error {
	return func(r io.Reader, opts selfupdate.Options) error {
		opts.TargetPath = target
		return selfupdate.Apply(r, opts)
	}
}

func TestApply_RealMinio_CorrectChecksumReplacesTargetAndRelaunches(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ritual")
	require.NoError(t, os.WriteFile(target, []byte("OLD-BINARY"), 0o755)) //nolint:gosec // test artifact

	const newBytes = "NEW-BINARY-BYTES"
	sum := sha256.Sum256([]byte(newBytes))

	remote := &mocks.MockStorageRepository{
		GetStreamFunc: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(newBytes)), nil
		},
	}
	u := New(remote, "2.0.0", "linux", "amd64", nil)
	u.applyFn = realApplyTo(target)
	relaunched := false
	u.relaunch = func() error { relaunched = true; return nil }

	err := u.Apply(t.Context(), ports.Update{Version: "2.1.0", Key: "k", SHA256: hex.EncodeToString(sum[:])})
	require.NoError(t, err)

	got, _ := os.ReadFile(target) //nolint:gosec // test artifact
	assert.Equal(t, newBytes, string(got), "real minio.Apply atomically replaced the target with the verified bytes")
	assert.True(t, relaunched)
}

func TestApply_RealMinio_ChecksumMismatchLeavesTargetIntact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ritual")
	require.NoError(t, os.WriteFile(target, []byte("OLD-BINARY"), 0o755)) //nolint:gosec // test artifact

	// sha256 of "expected", but the stream carries "corrupt" — minio must reject.
	wrong := sha256.Sum256([]byte("expected"))
	remote := &mocks.MockStorageRepository{
		GetStreamFunc: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("corrupt")), nil
		},
	}
	u := New(remote, "2.0.0", "linux", "amd64", nil)
	u.applyFn = realApplyTo(target)
	relaunched := false
	u.relaunch = func() error { relaunched = true; return nil }

	err := u.Apply(t.Context(), ports.Update{Version: "2.1.0", Key: "k", SHA256: hex.EncodeToString(wrong[:])})

	require.Error(t, err, "real minio.Apply must reject a checksum mismatch")
	got, _ := os.ReadFile(target) //nolint:gosec // test artifact
	assert.Equal(t, "OLD-BINARY", string(got), "rollback intact — the running binary is untouched on a bad download")
	assert.False(t, relaunched, "never relaunch after a failed replace")
}
