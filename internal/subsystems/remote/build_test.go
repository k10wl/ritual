package remote_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/subsystems/remote"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readerOf(s string) io.Reader { return strings.NewReader(s) }

func requireFileExists(t *testing.T, path, msg string) {
	t.Helper()
	_, err := os.Stat(path)
	require.NoErrorf(t, err, msg+" — stat error: %v", err)
}

func TestBuild_ModeMock_ReturnsThrottledMockBackedByRemoteMockDir(t *testing.T) {
	tempRoot := t.TempDir()
	originalRoot := config.RootPath
	config.RootPath = tempRoot
	defer func() { config.RootPath = originalRoot }()

	bus := adapters.NewEventBus(8)

	storage, err := remote.Build(t.Context(), remote.ModeMock, bus)

	require.NoError(t, err, "Build must succeed in ModeMock — alpha default that runs against local-FS without any credentials")
	require.NotNil(t, storage, "Build must return a non-nil storage so the composition-root decorator stack has something to wrap")

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	require.NoError(t, storage.PutStream(ctx, "smoke", readerOf("ok")), "the mock branch must be wired against <root>/remote-mock — a Put against a key must succeed and land on disk where seedRemoteWorld helpers expect it")

	mockKey := filepath.Join(tempRoot, "remote-mock", "smoke")
	requireFileExists(t, mockKey, "Put under ModeMock must materialise <root>/remote-mock/<key> on disk — without this seedRemoteWorld and assertRemoteLockAbsent helpers in the integration suite would not interoperate with Build")
}

func TestBuild_ModeR2WithoutEnvVars_ReturnsErrR2EnvIncompleteNamingMissingVars(t *testing.T) {
	clearR2Env(t)
	bus := adapters.NewEventBus(8)

	storage, err := remote.Build(t.Context(), remote.ModeR2, bus)

	assert.Nil(t, storage, "Build must return nil storage when ModeR2 is selected but env is incomplete — partial wiring would surface as confusing storage errors at first Put rather than a clear configuration failure at boot")
	require.Error(t, err, "Build must fail fast when ModeR2 is selected without credentials so the operator catches the misconfiguration before the GUI shows the Start button")
	assert.ErrorIs(t, err, remote.ErrR2EnvIncomplete, "Build must wrap the named sentinel so callers (and future cmd/gui error handling) can distinguish 'creds missing' from a real R2 connectivity failure")
	assert.Contains(t, err.Error(), remote.EnvR2Bucket, "error must name RITUAL_R2_BUCKET so the operator knows exactly which env var to set without consulting docs")
	assert.Contains(t, err.Error(), remote.EnvR2AccountID, "error must name RITUAL_R2_ACCOUNT_ID so the operator knows exactly which env var to set without consulting docs")
	assert.Contains(t, err.Error(), remote.EnvR2AccessKeyID, "error must name RITUAL_R2_ACCESS_KEY_ID so the operator knows exactly which env var to set without consulting docs")
	assert.Contains(t, err.Error(), remote.EnvR2SecretAccessKey, "error must name RITUAL_R2_SECRET_ACCESS_KEY so the operator knows exactly which env var to set without consulting docs")
}

func TestBuild_ModeR2WithEnvVars_AttemptsR2BranchAndReportsAdapterFailureCleanly(t *testing.T) {
	clearR2Env(t)
	t.Setenv(remote.EnvR2Bucket, "ritual-alpha")
	t.Setenv(remote.EnvR2AccountID, "fake-account")
	t.Setenv(remote.EnvR2AccessKeyID, "fake-key")
	t.Setenv(remote.EnvR2SecretAccessKey, "fake-secret")

	bus := adapters.NewEventBus(8)

	storage, err := remote.Build(t.Context(), remote.ModeR2, bus)

	if err != nil {
		assert.False(t, errors.Is(err, remote.ErrR2EnvIncomplete), "with all four env vars set the error must NOT be ErrR2EnvIncomplete — that sentinel is reserved for missing-cred misconfiguration, not adapter-side failures")
		assert.Contains(t, err.Error(), "remote: R2", "Build must wrap R2-branch errors with the 'remote: R2:' prefix so callers can distinguish factory-level failures from downstream storage errors raised once the run starts")
		return
	}
	require.NotNil(t, storage, "if NewR2Repository accepts the fake creds without I/O the factory must still return a non-nil storage — operational issues surface lazily on the first Get/Put rather than at construction time")
}

func clearR2Env(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		remote.EnvR2Bucket,
		remote.EnvR2AccountID,
		remote.EnvR2AccessKeyID,
		remote.EnvR2SecretAccessKey,
	} {
		t.Setenv(k, "")
	}
}
