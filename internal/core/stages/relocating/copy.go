package relocating

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"ritual/internal/adapters"
	"ritual/internal/config"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
)

// contentDirs is the CONTENT set from design-log/055's classification
// table — everything a relocate physically moves. objects/ and refs/ live
// on the "local" facade, server/ and worlds/ on the "workdir" facade,
// matching today's cmd/gui.buildRuntime split
// (adapters.NewFSRepository(wr, "local"/"workdir")).
var contentDirs = []string{"objects", "refs", "server", "worlds"}

type copyTarget struct {
	prefix string
	old    ports.StorageRepository
	newer  ports.StorageRepository
}

func copyTargets(refs WorkRootRefs, newLocal, newWorkdir ports.StorageRepository) []copyTarget {
	oldLocal := refs.Local.Current()
	oldWorkdir := refs.Workdir.Current()
	return []copyTarget{
		{"objects/", oldLocal, newLocal},
		{"refs/", oldLocal, newLocal},
		{"server/", oldWorkdir, newWorkdir},
		{"worlds/", oldWorkdir, newWorkdir},
	}
}

// buildNewRoot opens a fresh os.Root at dst (creating it if needed) and
// wraps it in the same FSRepository pair the composition root builds at
// boot ("local"/"workdir" labels for observability parity).
func buildNewRoot(dst string) (*os.Root, ports.StorageRepository, ports.StorageRepository, error) {
	if err := os.MkdirAll(dst, config.DirPermission); err != nil {
		return nil, nil, nil, fmt.Errorf("relocating: create destination: %w", err)
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("relocating: open destination root: %w", err)
	}
	local, err := adapters.NewFSRepository(root, "local")
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, fmt.Errorf("relocating: local storage: %w", err)
	}
	workdir, err := adapters.NewFSRepository(root, "workdir")
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, fmt.Errorf("relocating: workdir storage: %w", err)
	}
	return root, local, workdir, nil
}

// planCopy sums file count/bytes across objects/refs/server/worlds under
// the CURRENTLY active root, for the PlanInfo{BytesTotal,FilesTotal} event.
// Walks the raw *os.Root directly (same technique FSRepository.List already
// uses internally, and the same shape as cmd/gui's walkLocalPrefix) rather
// than via StorageRepository, because that interface has no Stat/size
// method — only List (keys). The actual copy in copyContent never uses this
// path; it is a size-estimation pass only.
func planCopy(refs WorkRootRefs) (totalBytes int64, totalFiles int, err error) {
	root := refs.Root.Load()
	for _, dir := range contentDirs {
		b, f, walkErr := walkPrefixSize(root, dir)
		if walkErr != nil {
			return 0, 0, walkErr
		}
		totalBytes += b
		totalFiles += f
	}
	return totalBytes, totalFiles, nil
}

func walkPrefixSize(root *os.Root, prefix string) (int64, int, error) {
	fsys := root.FS()
	info, err := fs.Stat(fsys, prefix)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("relocating: stat %s: %w", prefix, err)
	}
	if !info.IsDir() {
		return info.Size(), 1, nil
	}

	var bytes int64
	var count int
	walkErr := fs.WalkDir(fsys, prefix, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		bytes += fi.Size()
		count++
		return nil
	})
	if walkErr != nil {
		return 0, 0, fmt.Errorf("relocating: walk %s: %w", prefix, walkErr)
	}
	return bytes, count, nil
}

// copyContent lists every key under the CONTENT prefixes on the OLD
// local/workdir facades and streams each into newLocal/newWorkdir via
// GetStream->PutStream. StorageRepository.Copy is intra-repository only
// (FSRepository.Copy takes a single *os.Root receiver) so a cross-root
// transfer cannot use it; GetStream/PutStream is the only viable primitive
// (design-log/055 Q2). Periodically publishes ritual.UpdateInfo with a
// "percent" key. Checks ctx.Err() between files, never mid-stream, so a
// Stop finishes the in-flight file then aborts before starting the next.
func copyContent(ctx context.Context, refs WorkRootRefs, newLocal, newWorkdir ports.StorageRepository, bus ports.EventBus) error {
	targets := copyTargets(refs, newLocal, newWorkdir)

	keysPerTarget := make([][]string, len(targets))
	var totalKeys int
	for i, t := range targets {
		keys, err := t.old.List(ctx, t.prefix)
		if err != nil {
			return fmt.Errorf("relocating: list %s: %w", t.prefix, err)
		}
		keysPerTarget[i] = keys
		totalKeys += len(keys)
	}

	var done int
	for i, t := range targets {
		for _, key := range keysPerTarget[i] {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := copyKey(ctx, t.old, t.newer, key); err != nil {
				return fmt.Errorf("relocating: copy %s: %w", key, err)
			}
			done++
			if bus != nil && totalKeys > 0 {
				pct := float64(done) / float64(totalKeys) * 100
				bus.Publish(ritual.UpdateInfo{Operation: "relocate", Message: "copying", Data: map[string]any{"percent": pct}})
			}
		}
	}
	return nil
}

func copyKey(ctx context.Context, src, dst ports.StorageRepository, key string) error {
	rc, err := src.GetStream(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return dst.PutStream(ctx, key, rc)
}

// verify re-checks the destination before it becomes live. objects/ blobs
// are re-decoded and xxhash-verified against their filename for free by
// wrapping the destination in a CompressingStorage — GetStream through it
// performs the same zstd-decode + xxhash integrity check design-log/025
// already relies on for a corrupted-write detection, so a mismatch simply
// surfaces as an error on Close, no re-implementation needed. refs/ entries
// are re-parsed as JSON. server/worlds/ are not content-addressed, so they
// only get a presence + non-zero-size check.
// verify takes stopCtx (not the outer, uncancellable-by-Stop ctx) and polls
// ctx.Err() between keys — FSRepository.GetStream ignores its ctx parameter
// entirely, so this between-key check is the only way a Stop request during
// the (potentially long, for a large install) verify phase can actually
// take effect, mirroring copyContent's own cancellation granularity.
func verify(ctx context.Context, newLocal, newWorkdir ports.StorageRepository) error {
	compressed, err := adapters.NewCompressingStorage(newLocal)
	if err != nil {
		return fmt.Errorf("relocating: build verification decoder: %w", err)
	}

	objectKeys, err := newLocal.List(ctx, "objects/")
	if err != nil {
		return fmt.Errorf("relocating: list objects for verification: %w", err)
	}
	for _, key := range objectKeys {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyStreamNonEmpty(ctx, compressed, key); err != nil {
			return fmt.Errorf("relocating: verify object %s: %w", key, err)
		}
	}

	refKeys, err := newLocal.List(ctx, "refs/")
	if err != nil {
		return fmt.Errorf("relocating: list refs for verification: %w", err)
	}
	for _, key := range refKeys {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyRefParses(ctx, newLocal, key); err != nil {
			return fmt.Errorf("relocating: verify ref %s: %w", key, err)
		}
	}

	for _, dir := range []string{"server/", "worlds/"} {
		keys, err := newWorkdir.List(ctx, dir)
		if err != nil {
			return fmt.Errorf("relocating: list %s for verification: %w", dir, err)
		}
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := verifyStreamNonEmpty(ctx, newWorkdir, key); err != nil {
				return fmt.Errorf("relocating: verify %s: %w", dir, err)
			}
		}
	}

	return nil
}

// verifyStreamNonEmpty reads key fully and checks Close's error, not just
// the read's. This is load-bearing for objects/ keys: CompressingStorage's
// integrity check (zstd-decode + xxhash-vs-filename) only surfaces a
// mismatch from Close (internal/adapters/compressing.go), so a deferred,
// discarded Close would silently accept a corrupted blob.
func verifyStreamNonEmpty(ctx context.Context, store ports.StorageRepository, key string) error {
	rc, err := store.GetStream(ctx, key)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(io.Discard, rc)
	closeErr := rc.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n == 0 {
		return fmt.Errorf("empty file: %s", key)
	}
	return nil
}

func verifyRefParses(ctx context.Context, store ports.StorageRepository, key string) error {
	rc, err := store.GetStream(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	var ref domain.Ref
	if err := json.Unmarshal(raw, &ref); err != nil {
		return err
	}
	if ref.Timestamp == "" {
		return fmt.Errorf("ref %s has no timestamp", key)
	}
	return nil
}
