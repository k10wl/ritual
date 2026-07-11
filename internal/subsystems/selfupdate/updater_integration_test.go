package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
	"strings"
	"testing"

	"github.com/minio/selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestCheckThenApply_RealMinio_EndToEnd is the full pipeline against a mocked
// remote holding ONE artifact at a version higher than the running binary:
// Check discovers it as outdated, and the Update it returns drives Apply
// straight into the real minio replace. This is the test that proves the
// pieces compose — specifically that the sha256 Check parses out of the key
// leaf is the same digest that verifies the bytes Apply downloads. (Separate
// Check/Apply unit tests can't catch a key↔checksum plumbing mismatch.)
func TestCheckThenApply_RealMinio_EndToEnd(t *testing.T) {
	const newBytes = "NEW-RITUAL-v2.1.0-PAYLOAD"
	sum := sha256.Sum256([]byte(newBytes))
	shaHex := hex.EncodeToString(sum[:])

	// One artifact on the remote, keyed by its own sha (no ext on linux).
	key := PrefixFor("linux", "amd64") + "2.1.0/" + shaHex
	remote := &mocks.MockStorageRepository{
		ListFunc: func(_ context.Context, prefix string) ([]string, error) {
			assert.Equal(t, "bin/linux-amd64/", prefix, "Check lists this client's own platform prefix")
			return []string{key}, nil
		},
		GetStreamFunc: func(_ context.Context, k string) (io.ReadCloser, error) {
			require.Equal(t, key, k, "Apply downloads exactly the key Check resolved")
			return io.NopCloser(strings.NewReader(newBytes)), nil
		},
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "ritual")
	require.NoError(t, os.WriteFile(target, []byte("OLD-RITUAL-v2.0.0"), 0o755)) //nolint:gosec // test artifact

	u := New(remote, "2.0.0", "linux", "amd64", nil)
	u.applyFn = realApplyTo(target)
	relaunched := false
	u.relaunch = func() error { relaunched = true; return nil }

	// 1. Check: a higher remote version reads as outdated.
	up, outdated, err := u.Check(t.Context())
	require.NoError(t, err)
	require.True(t, outdated, "remote 2.1.0 > running 2.0.0")
	require.Equal(t, "2.1.0", up.Version)
	require.Equal(t, shaHex, up.SHA256, "checksum comes straight from the key leaf")

	// 2. Apply the very Update Check produced — real minio verifies + replaces.
	require.NoError(t, u.Apply(t.Context(), up))

	got, _ := os.ReadFile(target) //nolint:gosec // test artifact
	assert.Equal(t, newBytes, string(got),
		"end to end: Check→Apply downloaded the higher version and the sha from its key verified the bytes")
	assert.True(t, relaunched, "a clean update relaunches into the new binary")
}

// TestCheck_VersionMatrix_AgainstMockRemote covers the full decision matrix
// against a mock remote holding exactly one artifact: only a strictly-higher
// remote version is outdated; same and lower must NOT trigger an update (the
// flow returns before Apply). Running binary is 2.0.0.
func TestCheck_VersionMatrix_AgainstMockRemote(t *testing.T) {
	cases := []struct {
		name         string
		remoteVer    string
		wantOutdated bool
	}{
		{"lower remote — never update", "1.9.0", false},
		{"same remote — never update", "2.0.0", false},
		{"higher remote — update", "2.1.0", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := PrefixFor("linux", "amd64") + tc.remoteVer + "/deadbeef"
			applied := false
			remote := &mocks.MockStorageRepository{
				ListFunc: func(_ context.Context, _ string) ([]string, error) { return []string{key}, nil },
				GetStreamFunc: func(_ context.Context, _ string) (io.ReadCloser, error) {
					applied = true // must never be hit when not outdated
					return io.NopCloser(strings.NewReader("x")), nil
				},
			}
			u := New(remote, "2.0.0", "linux", "amd64", nil)

			up, outdated, err := u.Check(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tc.wantOutdated, outdated)
			assert.Equal(t, tc.remoteVer, up.Version, "Check always reports the latest it found, even when not outdated")

			// Mirror runUpdateFlow: only Apply when outdated. Prove the not-outdated
			// branch never downloads.
			if outdated {
				u.applyFn = func(io.Reader, selfupdate.Options) error { return nil }
				require.NoError(t, u.Apply(t.Context(), up))
				assert.True(t, applied, "outdated → Apply downloads")
			} else {
				assert.False(t, applied, "same/lower remote must not download or replace anything")
			}
		})
	}
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
