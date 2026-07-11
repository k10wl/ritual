// Package remote owns the composition of the remote storage stack.
// Build returns a single ports.StorageRepository — local-FS mock when
// Mode is ModeMock (alpha default), real Cloudflare R2 when Mode is
// ModeR2. The composition root calls Build once and feeds the result
// into the blob-store decorator chain (compressing → counter →
// observed → PrefixRouter) without knowing or caring which backend
// it got.
//
// Single-port-out keeps this within the subsystem-extraction rules
// (memory feedback_composition_root_shape).
//
// R2 credentials are read from environment variables at Build time —
// never from settings.json, never hardcoded. The four required vars
// (RITUAL_R2_BUCKET, RITUAL_R2_ACCOUNT_ID, RITUAL_R2_ACCESS_KEY_ID,
// RITUAL_R2_SECRET_ACCESS_KEY) live in the operator's shell or CI
// secret store, so a leaked repo or screenshotted settings file
// cannot expose them. Mock vs R2 is a runtime toggle on
// RITUAL_REMOTE_MODE (design-log/030) — no rebuild required.
package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/ports"
)

// Mode selects the remote-storage backend at composition time.
type Mode int

const (
	// ModeMock points the remote stack at <root>/remote-mock under a
	// throttled local-FS adapter. Alpha default; no credentials needed.
	ModeMock Mode = iota
	// ModeR2 points the remote stack at Cloudflare R2 via the S3 API.
	// Requires RITUAL_R2_{BUCKET,ACCOUNT_ID,ACCESS_KEY_ID,SECRET_ACCESS_KEY}
	// in the environment of the process that launches the GUI.
	ModeR2
)

// MockBytesPerSecond throttles the local-FS mock to ~100 Mbps so the
// dev loop reflects realistic push/pull pacing instead of native disk
// speed (audit fix #12).
const MockBytesPerSecond = 12_500_000

// Required env-var names for ModeR2. Surfaced here as constants so a
// hand-off agent can grep for them without reading the wiring.
const (
	EnvR2Bucket          = "RITUAL_R2_BUCKET"
	EnvR2AccountID       = "RITUAL_R2_ACCOUNT_ID"
	EnvR2AccessKeyID     = "RITUAL_R2_ACCESS_KEY_ID"
	EnvR2SecretAccessKey = "RITUAL_R2_SECRET_ACCESS_KEY"
)

// Baked-in R2 credentials, injected at build time via -ldflags
// -X ritual/internal/subsystems/remote.bakedBucket=... etc. The Taskfile
// reads .env.{RITUAL_ENV}.local at build time and injects the matching
// profile's creds into the binary, so the shipped .exe is self-contained
// — no env file needs to travel with it.
//
// Env vars still win at runtime (godotenv-style precedence) so an operator
// can override a single profile's creds without rebuilding; the baked
// values are only the fallback when the env var is empty.
//
// Tradeoff: anyone with the binary can `strings` it to extract these creds.
// Scope them narrowly (e.g. R2 tokens limited to the bin/ prefix the
// updater needs) — design-log/037 §publish covers the access model.
var (
	bakedBucket          string
	bakedAccountID       string
	bakedAccessKeyID     string
	bakedSecretAccessKey string
)

// bakedRemoteMode bakes the storage backend into the binary at build time
// via -ldflags -X ritual/internal/subsystems/remote.bakedRemoteMode=mock
// (set by gui:build:dev:local — design-log/048). "mock" selects the local-FS
// backend; empty (the default, and every `go run` of cmd/publish) falls
// through to ModeR2. The RITUAL_REMOTE_MODE env var still wins over this
// baked value (ResolveMode), mirroring the env-over-baked creds precedence —
// so a baked-local binary can be forced back to R2 without a rebuild.
var bakedRemoteMode string

// EnvRemoteMode toggles the remote backend at runtime. Values: "mock"
// → ModeMock; anything else (including unset) → ModeR2. Documented as
// the way to opt into the dev-only local-FS mock without rebuilding
// (design-log/030).
const EnvRemoteMode = "RITUAL_REMOTE_MODE"

// ResolveMode returns the Mode for this process. Precedence (design-log/048):
//  1. RITUAL_REMOTE_MODE env, if set — "mock" → ModeMock, anything else → ModeR2.
//  2. bakedRemoteMode ldflag, if "mock" → ModeMock.
//  3. default ModeR2 — production posture per design-log/030.
//
// Env wins over baked so an operator can override a baked-local binary back to
// R2 (or vice-versa) without rebuilding, symmetric with the baked-creds rule.
func ResolveMode() Mode {
	if v, ok := os.LookupEnv(EnvRemoteMode); ok && v != "" {
		if v == "mock" {
			return ModeMock
		}
		return ModeR2
	}
	if bakedRemoteMode == "mock" {
		return ModeMock
	}
	return ModeR2
}

// ErrR2EnvIncomplete is returned by Build when ModeR2 is selected but
// any of the four required env vars is empty. The error names which
// vars are missing so the operator can fix their shell without
// guesswork.
var ErrR2EnvIncomplete = errors.New("remote: ModeR2 requires R2 credentials in environment")

// Build returns the raw remote storage selected by mode. bus is
// forwarded into adapters.NewR2Repository for per-op lifecycle events
// on the R2 branch; the mock branch ignores it (the outer composition
// root layers observed.NewStorage downstream of Build, so both
// branches end up with the same observed shape externally).
func Build(ctx context.Context, mode Mode, bus ports.EventBus) (ports.StorageRepository, error) {
	if mode == ModeR2 {
		return buildR2FromEnv(ctx, bus)
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

func buildR2FromEnv(ctx context.Context, bus ports.EventBus) (ports.StorageRepository, error) {
	// Env wins, baked-in falls back. So an operator can override a single
	// cred via shell export without rebuilding, but the shipped binary
	// still starts on a fresh machine with no env file in sight.
	bucket := firstNonEmpty(os.Getenv(EnvR2Bucket), bakedBucket)
	accountID := firstNonEmpty(os.Getenv(EnvR2AccountID), bakedAccountID)
	accessKeyID := firstNonEmpty(os.Getenv(EnvR2AccessKeyID), bakedAccessKeyID)
	secretAccessKey := firstNonEmpty(os.Getenv(EnvR2SecretAccessKey), bakedSecretAccessKey)

	var missing []string
	if bucket == "" {
		missing = append(missing, EnvR2Bucket)
	}
	if accountID == "" {
		missing = append(missing, EnvR2AccountID)
	}
	if accessKeyID == "" {
		missing = append(missing, EnvR2AccessKeyID)
	}
	if secretAccessKey == "" {
		missing = append(missing, EnvR2SecretAccessKey)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: missing %v", ErrR2EnvIncomplete, missing)
	}

	repo, err := adapters.NewR2Repository(ctx, bucket, accountID, accessKeyID, secretAccessKey, bus)
	if err != nil {
		return nil, fmt.Errorf("remote: R2: %w", err)
	}
	return repo, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func mockRemoteDir() (string, error) {
	dir := config.RootPath + string(os.PathSeparator) + "remote-mock"
	if err := os.MkdirAll(dir, config.DirPermission); err != nil {
		return "", fmt.Errorf("remote: create mock dir: %w", err)
	}
	return dir, nil
}
