package remote_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
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

func TestBuild_NilR2_ReturnsThrottledMockBackedByRemoteMockDir(t *testing.T) {
	tempRoot := t.TempDir()
	originalRoot := config.RootPath
	config.RootPath = tempRoot
	defer func() { config.RootPath = originalRoot }()

	settings := domain.DefaultSettings()
	bus := adapters.NewEventBus(8)

	storage, err := remote.Build(t.Context(), settings, bus)

	require.NoError(t, err, "Build must succeed against default settings — alpha operators run against local-FS mock until they drop R2 creds into settings.json")
	require.NotNil(t, storage, "Build must return a non-nil storage so the composition-root decorator stack has something to wrap")

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	require.NoError(t, storage.PutStream(ctx, "smoke", readerOf("ok")), "the mock branch must be wired against <root>/remote-mock — a Put against a key must succeed and land on disk where seedRemoteWorld helpers expect it")

	mockKey := filepath.Join(tempRoot, "remote-mock", "smoke")
	requireFileExists(t, mockKey, "Put under the mock branch must materialise <root>/remote-mock/<key> on disk — without this seedRemoteWorld and assertRemoteLockAbsent helpers in the integration suite would not interoperate with Build")
}

func TestBuild_PopulatedR2_AttemptsR2BranchAndReturnsAdapterError(t *testing.T) {
	settings := domain.DefaultSettings()
	settings.RemoteR2 = &domain.R2Config{
		Bucket:          "ritual-alpha",
		AccountID:       "fake-account",
		AccessKeyID:     "fake-key",
		SecretAccessKey: "fake-secret",
	}
	bus := adapters.NewEventBus(8)

	storage, err := remote.Build(t.Context(), settings, bus)

	if err != nil {
		assert.Contains(t, err.Error(), "remote: R2", "Build must wrap R2-branch errors with the 'remote: R2:' prefix so callers can distinguish factory-level failures (cred shape, network reachability) from downstream storage errors raised once the run starts")
		return
	}
	require.NotNil(t, storage, "if NewR2Repository accepts the fake creds without I/O the factory must still return a non-nil storage — operational issues surface lazily on the first Get/Put rather than at construction time")
}
