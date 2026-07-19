// Command publish uploads a built Ritual binary to the R2 update channel
// (design-log/037 Phase F). Layout: bin/<goos>-<goarch>/<version>/<sha256>[.exe]
// — the sha256 is the object's leaf name, so integrity is intrinsic to the key
// (no feed file, no sidecar). The client's selfupdate.Check lists the prefix
// and picks the highest semver.
//
// The version is NOT a flag: it is read from config.AppVersion — the same
// compiled const the GUI binary bakes at this commit — so the published
// <version> path can never advertise a version the bytes don't self-report
// (the anti-loop invariant). `task publish` always rebuilds first, so the
// artifact here is current.
//
// Reuses the app's own R2 adapter (remote.Build + RITUAL_R2_* env), so reader
// and writer share one transport and one key layout — no `aws s3 cp` drift.
//
// Usage: go run ./cmd/publish -artifact bin/ritual.exe -os windows -arch amd64
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/ports"
	"ritual/internal/subsystems/remote"
	"ritual/internal/subsystems/selfupdate"
)

func main() {
	artifact := flag.String("artifact", "", "path to the built binary to publish (e.g. bin/ritual.exe)")
	goos := flag.String("os", "", "target GOOS the artifact was built for (e.g. windows)")
	goarch := flag.String("arch", "", "target GOARCH the artifact was built for (e.g. amd64)")
	// Explicit R2 credential flags — see buildStorage's doc comment for why
	// these exist instead of just relying on remote.Build's RITUAL_R2_* env
	// resolution.
	bucket := flag.String("bucket", "", "R2 bucket name (bypasses env/mock resolution when set, alongside -account/-access-key/-secret-key)")
	account := flag.String("account", "", "R2 account ID")
	accessKey := flag.String("access-key", "", "R2 access key ID")
	secretKey := flag.String("secret-key", "", "R2 secret access key")
	flag.Parse()

	if err := run(*artifact, *goos, *goarch, *bucket, *account, *accessKey, *secretKey); err != nil {
		fmt.Fprintf(os.Stderr, "publish: %v\n", err)
		os.Exit(1)
	}
}

func run(artifact, goos, goarch, bucket, account, accessKey, secretKey string) error {
	if artifact == "" || goos == "" || goarch == "" {
		return errors.New("-artifact, -os and -arch are all required")
	}
	version := config.AppVersion // same const the binary baked — anti-loop invariant

	sum, err := sha256File(artifact)
	if err != nil {
		return err
	}

	ctx := context.Background()
	// Mirror the GUI's transport: a bus drained to stderr surfaces R2 retry
	// noise during the upload.
	bus := adapters.NewEventBus(64)
	go drainToStderr(ctx, bus)
	storage, err := buildStorage(ctx, bus, bucket, account, accessKey, secretKey)
	if err != nil {
		return fmt.Errorf("build remote: %w", err)
	}

	prefix := selfupdate.PrefixFor(goos, goarch)
	versionDir := prefix + version + "/"
	key := versionDir + sum + filepath.Ext(artifact) // leaf basename = sha256

	if err := putFile(ctx, storage, key, artifact); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "publish: uploaded %s (%s)\n", key, version)

	// Write-new-then-sweep-old (mirrors refs.Committer.finalizeAmend): the new
	// artifact is durable before any prior same-version object is removed, so a
	// crash never empties the version dir. Older *versions* are kept (rollback).
	if err := sweepStaleSiblings(ctx, storage, versionDir, key); err != nil {
		return fmt.Errorf("sweep stale siblings: %w", err)
	}
	return nil
}

// buildStorage prefers explicit R2 credentials over remote.Build's env/mock
// resolution when bucket is non-empty.
//
// Why: `task publish:prod` runs as `RITUAL_ENV=prod task _publish`, a nested
// `task` subprocess spawned from an outer, unprefixed `task publish:prod`
// invocation that itself resolved RITUAL_ENV to its "dev" default and
// exported RITUAL_R2_BUCKET=ritual-dev (etc.) into its own process
// environment via the Taskfile's file-level `env:` block. The nested process
// inherits that already-set RITUAL_R2_BUCKET from its parent's OS
// environment, and its own (correctly re-resolved to prod) `env:` block does
// not override an already-present env var of the same name — so
// remote.Build's os.Getenv(RITUAL_R2_BUCKET) silently reads the wrong
// bucket regardless of RITUAL_ENV. Confirmed live: a "prod" publish landed
// in the "ritual-dev" bucket.
//
// Task's own template variables ({{.R2_BUCKET_NAME}} etc.) are unaffected by
// that inheritance — they come from Task's per-process dotenv load, not the
// OS environment. The Taskfile now passes them straight through as flags,
// sidestepping the env-inheritance problem entirely. The mock path (no
// bucket flag) is untouched and still goes through remote.Build.
func buildStorage(ctx context.Context, bus ports.EventBus, bucket, account, accessKey, secretKey string) (ports.StorageRepository, error) {
	if bucket != "" {
		return adapters.NewR2Repository(ctx, bucket, account, accessKey, secretKey, bus)
	}
	return remote.Build(ctx, remote.ResolveMode(), bus)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- artifact path is a build-time flag, not user input
	if err != nil {
		return "", fmt.Errorf("open artifact %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash artifact %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func putFile(ctx context.Context, storage ports.StorageRepository, key, path string) error {
	f, err := os.Open(path) // #nosec G304 -- artifact path is a build-time flag, not user input
	if err != nil {
		return fmt.Errorf("open artifact %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := storage.PutStream(ctx, key, f); err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// sweepStaleSiblings deletes every object under versionDir except keep — a
// prior re-publish of the same version with different bytes (different sha) —
// so the version dir holds exactly one artifact and latest() stays unambiguous.
func sweepStaleSiblings(ctx context.Context, storage ports.StorageRepository, versionDir, keep string) error {
	keys, err := storage.List(ctx, versionDir)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k == keep {
			continue
		}
		if err := storage.Delete(ctx, k); err != nil {
			return fmt.Errorf("delete stale %s: %w", k, err)
		}
		fmt.Fprintf(os.Stderr, "publish: swept stale %s\n", k)
	}
	return nil
}

func drainToStderr(ctx context.Context, bus ports.EventBus) {
	ch, unsub := bus.Subscribe()
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "  · %s\n", evt)
		}
	}
}
