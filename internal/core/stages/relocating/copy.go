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
	"sync"
	"sync/atomic"
	"time"
)

// relocateHeartbeat is how often copyContent publishes a RelocateProgress
// even when no file has completed since the last publish — see
// RelocateProgress's doc comment for why a large single file needs this. A
// var (not const), mirroring progress.Ticker's exported Alpha/WindowN
// tunables, so a white-box test can shrink it and observe a mid-file
// heartbeat without waiting out a real 500ms per test run.
var relocateHeartbeat = 500 * time.Millisecond

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
// the CURRENTLY active root, for the "planned" UpdateInfo event
// (strategy.go) carrying {bytesTotal,filesTotal}.
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
// (design-log/055 Q2). Destination writes go through a CounterStorage tap
// (internal/adapters/counter.go, the same decorator pull/push tap for
// progress.Ticker) so BytesDone reflects bytes actually flushed to disk as
// io.Copy streams them, not a per-file estimate — RelocateProgress
// publishes both after each file completes and on a relocateHeartbeat
// ticker, so a single large file doesn't freeze the dial for its whole
// transfer (design-log/056 follow-up, 2026-08-15). Checks ctx.Err() between
// files, never mid-stream, so a Stop finishes the in-flight file then
// aborts before starting the next.
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

	counters := &adapters.StorageCounters{}
	for i := range targets {
		targets[i].newer = adapters.NewCounterStorage(targets[i].newer, counters)
	}

	start := time.Now()
	var filesDone atomic.Int64
	publish := func() {
		if bus != nil && totalKeys > 0 {
			bus.Publish(RelocateProgress{
				FilesDone:  int(filesDone.Load()),
				FilesTotal: totalKeys,
				BytesDone:  counters.BytesOut.Load(),
				Elapsed:    time.Since(start),
			})
		}
	}

	var wg sync.WaitGroup
	stopHeartbeat := make(chan struct{})
	if bus != nil && totalKeys > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(relocateHeartbeat)
			defer ticker.Stop()
			for {
				select {
				case <-stopHeartbeat:
					return
				case <-ticker.C:
					publish()
				}
			}
		}()
	}
	defer func() {
		close(stopHeartbeat)
		wg.Wait()
	}()

	for i, t := range targets {
		for _, key := range keysPerTarget[i] {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := copyKey(ctx, t.old, t.newer, key); err != nil {
				return fmt.Errorf("relocating: copy %s: %w", key, err)
			}
			filesDone.Add(1)
			publish()
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

// verify re-checks the destination before it becomes live, but only for the
// two paths that need something beyond what copyContent already guarantees:
// objects/ blobs are re-decoded and xxhash-verified against their filename
// for free by wrapping the destination in a CompressingStorage — GetStream
// through it performs the same zstd-decode + xxhash integrity check
// design-log/025 already relies on for a corrupted-write detection, so a
// mismatch simply surfaces as an error on Close, no re-implementation
// needed. refs/ entries are re-parsed as JSON — a validity check copy can't
// provide. server/worlds/ get no separate verify pass (dropped 2026-08-11):
// they're not content-addressed, so there is nothing to check beyond "did
// the bytes arrive," and copyContent already surfaces any GetStream/
// PutStream error immediately, aborting before verify ever runs — a second
// read-through would only re-derive a success signal copy already gave. (A
// prior non-zero-size check here was also wrong on its own terms: a real
// world save legitimately contains dozens of 0-byte `.mca` region files —
// lazily-allocated POI/entity/region files for sparsely-generated areas
// like the Nether.)
// verify takes stopCtx (not the outer, uncancellable-by-Stop ctx) and polls
// ctx.Err() between keys — FSRepository.GetStream ignores its ctx parameter
// entirely, so this between-key check is the only way a Stop request during
// the (potentially long, for a large install) verify phase can actually
// take effect, mirroring copyContent's own cancellation granularity.
func verify(ctx context.Context, newLocal ports.StorageRepository) error {
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
		if err := verifyStreamIntegrity(ctx, compressed, key); err != nil {
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

	return nil
}

// verifyStreamIntegrity reads an objects/ key fully through the
// CompressingStorage-wrapped store and checks Close's error, WITHOUT
// rejecting a zero-byte result — a real 0-byte object is legitimate
// (xxhash64("") == "ef46db3751d8e999" is a real, valid object key).
// CompressingStorage's integrity check (zstd-decode + xxhash-vs-filename)
// still surfaces a mismatch from Close (internal/adapters/compressing.go)
// regardless of length, so a discarded Close would silently accept a
// corrupted blob — this keeps that check.
func verifyStreamIntegrity(ctx context.Context, store ports.StorageRepository, key string) error {
	rc, err := store.GetStream(ctx, key)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(io.Discard, rc)
	closeErr := rc.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
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
