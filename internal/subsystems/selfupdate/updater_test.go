package selfupdate

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/minio/selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ritual/internal/core/ports"
	"ritual/internal/core/ports/mocks"
)

func listing(keys []string, err error) *mocks.MockStorageRepository {
	return &mocks.MockStorageRepository{
		ListFunc: func(_ context.Context, _ string) ([]string, error) { return keys, err },
	}
}

func TestCheck_OutdatedWhenRemoteNewer(t *testing.T) {
	remote := listing([]string{"bin/windows-amd64/2.1.0/abc.exe"}, nil)
	u := New(remote, "2.0.0", "windows", "amd64", nil)

	up, outdated, err := u.Check(t.Context())

	require.NoError(t, err)
	assert.True(t, outdated, "remote 2.1.0 > running 2.0.0 must read as outdated")
	assert.Equal(t, "2.1.0", up.Version)
	assert.Equal(t, "bin/windows-amd64/2.1.0/abc.exe", up.Key)
	assert.Equal(t, "abc", up.SHA256)
}

func TestCheck_UpToDateWhenEqualOrAhead(t *testing.T) {
	remote := listing([]string{"bin/windows-amd64/2.0.0/abc.exe"}, nil)
	u := New(remote, "2.0.0", "windows", "amd64", nil)

	_, outdated, err := u.Check(t.Context())

	require.NoError(t, err)
	assert.False(t, outdated, "equal versions are not outdated")
}

func TestCheck_EmptyPrefixIsNotOutdated(t *testing.T) {
	u := New(listing(nil, nil), "2.0.0", "windows", "amd64", nil)

	up, outdated, err := u.Check(t.Context())

	require.NoError(t, err)
	assert.False(t, outdated)
	assert.Equal(t, ports.Update{}, up, "no artifacts → zero Update, never outdated")
}

func TestCheck_ListErrorPropagates(t *testing.T) {
	u := New(listing(nil, errors.New("r2: connection refused")), "2.0.0", "windows", "amd64", nil)

	_, _, err := u.Check(t.Context())

	require.Error(t, err, "a real listing failure must propagate, not masquerade as up-to-date")
}

func TestApply_StreamsBytesWithDecodedChecksumThenRelaunches(t *testing.T) {
	const body = "new-binary-bytes"
	remote := &mocks.MockStorageRepository{
		GetStreamFunc: func(_ context.Context, key string) (io.ReadCloser, error) {
			assert.Equal(t, "bin/windows-amd64/2.1.0/9f86d0.exe", key, "downloads the resolved artifact key")
			return io.NopCloser(strings.NewReader(body)), nil
		},
	}
	u := New(remote, "2.0.0", "windows", "amd64", nil)

	var gotBytes []byte
	var gotChecksum []byte
	u.applyFn = func(r io.Reader, opts selfupdate.Options) error {
		gotBytes, _ = io.ReadAll(r)
		gotChecksum = opts.Checksum
		return nil
	}
	relaunched := false
	u.relaunch = func() error { relaunched = true; return nil }

	err := u.Apply(t.Context(), ports.Update{Version: "2.1.0", Key: "bin/windows-amd64/2.1.0/9f86d0.exe", SHA256: "9f86d0"})

	require.NoError(t, err)
	assert.Equal(t, body, string(gotBytes), "the streamed artifact bytes reach minio unbuffered")
	want, _ := hex.DecodeString("9f86d0")
	assert.Equal(t, want, gotChecksum, "the hex sha from the key leaf is decoded and passed as the checksum")
	assert.True(t, relaunched, "a successful apply relaunches")
}

func TestApply_ChecksumMismatchDoesNotRelaunch(t *testing.T) {
	remote := &mocks.MockStorageRepository{
		GetStreamFunc: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("corrupt")), nil
		},
	}
	u := New(remote, "2.0.0", "windows", "amd64", nil)
	u.applyFn = func(io.Reader, selfupdate.Options) error {
		return errors.New("updating: checksum mismatch") // minio rolled back; binary intact
	}
	relaunched := false
	u.relaunch = func() error { relaunched = true; return nil }

	err := u.Apply(t.Context(), ports.Update{Version: "2.1.0", Key: "k", SHA256: "9f86d0"})

	require.Error(t, err)
	assert.False(t, relaunched, "a failed replace must never relaunch into a half-written binary")
}

func TestApply_BadHexChecksumRejectedBeforeDownload(t *testing.T) {
	downloaded := false
	remote := &mocks.MockStorageRepository{
		GetStreamFunc: func(_ context.Context, _ string) (io.ReadCloser, error) {
			downloaded = true
			return io.NopCloser(strings.NewReader("x")), nil
		},
	}
	u := New(remote, "2.0.0", "windows", "amd64", nil)

	err := u.Apply(t.Context(), ports.Update{Version: "2.1.0", Key: "k", SHA256: "zzz-not-hex"})

	require.Error(t, err)
	assert.False(t, downloaded, "a malformed checksum fails fast — no pointless download")
}
