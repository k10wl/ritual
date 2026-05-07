// Package remote owns the composition of the remote storage stack.
// Build returns a single ports.StorageRepository — local-FS mock when
// settings.RemoteR2 is nil, real Cloudflare R2 when populated. The
// composition root calls Build once and feeds the result into the
// blob-store decorator chain (compressing → counter → observed →
// PrefixRouter) without knowing or caring which backend it got.
//
// Single-port-out keeps this within the subsystem-extraction rules
// (memory feedback_composition_root_shape).
package remote

import (
	"context"
	"fmt"
	"os"

	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// MockBytesPerSecond throttles the local-FS mock to ~100 Mbps so the
// dev loop reflects realistic push/pull pacing instead of native disk
// speed (audit fix #12).
const MockBytesPerSecond = 12_500_000

// Build returns the raw remote storage. mockRoot is only consulted when
// settings.RemoteR2 is nil. bus is forwarded into adapters.NewR2Repository
// for per-op lifecycle events; it is not used on the mock branch (the
// outer composition root layers observed.NewStorage downstream of Build,
// so both branches end up with the same observed shape externally).
func Build(ctx context.Context, settings *domain.Settings, bus ports.EventBus) (ports.StorageRepository, error) {
	if settings.RemoteR2 != nil {
		return buildR2(ctx, settings.RemoteR2, bus)
	}
	return buildMock()
}

func buildMock() (ports.StorageRepository, error) {
	mockDir, err := mockRemoteDir()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(mockDir)
	if err != nil {
		return nil, fmt.Errorf("remote: open mock root: %w", err)
	}
	rawFS, err := adapters.NewFSRepository(root, "remote")
	if err != nil {
		return nil, fmt.Errorf("remote: mock FSRepository: %w", err)
	}
	return adapters.NewThrottledStorage(rawFS, MockBytesPerSecond), nil
}

func buildR2(ctx context.Context, r2 *domain.R2Config, bus ports.EventBus) (ports.StorageRepository, error) {
	repo, err := adapters.NewR2Repository(ctx, r2.Bucket, r2.AccountID, r2.AccessKeyID, r2.SecretAccessKey, bus)
	if err != nil {
		return nil, fmt.Errorf("remote: R2: %w", err)
	}
	return repo, nil
}

func mockRemoteDir() (string, error) {
	dir := config.RootPath + string(os.PathSeparator) + "remote-mock"
	if err := os.MkdirAll(dir, config.DirPermission); err != nil {
		return "", fmt.Errorf("remote: create mock dir: %w", err)
	}
	return dir, nil
}
