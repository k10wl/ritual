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
	flag.Parse()

	if err := run(*artifact, *goos, *goarch); err != nil {
		fmt.Fprintf(os.Stderr, "publish: %v\n", err)
		os.Exit(1)
	}
}

func run(artifact, goos, goarch string) error {
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
	// noise during the upload. remote.Build reads RITUAL_R2_* (or mock).
	bus := adapters.NewEventBus(64)
	go drainToStderr(ctx, bus)
	storage, err := remote.Build(ctx, remote.ResolveMode(), bus)
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
