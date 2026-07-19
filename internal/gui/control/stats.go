package control

import (
	"context"
	"sync"
	"time"
)

// statsCacheTTL is the freshness window for GetLocalStorageStats
// (design-log/045 §E §OQ3). Pane visits commonly re-fire within a few seconds;
// the cache amortises the FS walk without lying about a number the user can
// actually affect.
const statsCacheTTL = 5 * time.Second

// statsReadTimeout bounds the FS walk so a slow disk can't hang the Versions
// header. A walk of objects/* + per-blob Stat is fast (no opens, just dir
// metadata); ten seconds is comfortable on a healthy disk.
const statsReadTimeout = 10 * time.Second

// LocalStorageStats reports honest, dedup-aware disk usage for the local
// object store (design-log/045 §E). BytesOnDisk is the sum of every blob file
// under local objects/ — the number the user can compare against their disk's
// free space. ObjectCount is the unique blob count; with content-addressed
// dedup it is well below the sum of per-version logical bytes when versions
// share content.
type LocalStorageStats struct {
	BytesOnDisk int64 `json:"bytesOnDisk"`
	ObjectCount int   `json:"objectCount"`
}

// StorageStatFn walks a prefix in the local store and returns the byte sum +
// file count under it. Injected so control owns no FS code; the composition
// root wraps `<root>/local/<prefix>` via `os.Root` (sandbox-respecting).
type StorageStatFn func(ctx context.Context, prefix string) (bytes int64, count int, err error)

// statsCache holds the last-good GetLocalStorageStats result and its
// timestamp. Cheap mutex (sub-microsecond) on a one-shot read.
type statsCache struct {
	mu     sync.Mutex
	val    LocalStorageStats
	at     time.Time
	hasVal bool
}

// GetLocalStorageStats walks local objects/ and returns the total bytes +
// object count (design-log/045 §E). Cached for statsCacheTTL so rapid pane
// visits don't hammer disk; the cache invalidates on DeleteLocalVersion and
// ApplyRetentionNow success so the number is fresh after the user's own
// action (§OQ3). A nil stat fn, timeout, or read error returns the zero
// value — the header simply omits the line, never an error.
func (c *ControlService) GetLocalStorageStats() LocalStorageStats {
	if c.statsFn == nil {
		return LocalStorageStats{}
	}
	c.stats.mu.Lock()
	if c.stats.hasVal && time.Since(c.stats.at) < statsCacheTTL {
		v := c.stats.val
		c.stats.mu.Unlock()
		return v
	}
	c.stats.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), statsReadTimeout)
	defer cancel()
	bytes, count, err := c.statsFn(ctx, "objects/")
	if err != nil {
		return LocalStorageStats{}
	}
	v := LocalStorageStats{BytesOnDisk: bytes, ObjectCount: count}
	c.stats.mu.Lock()
	c.stats.val = v
	c.stats.at = time.Now()
	c.stats.hasVal = true
	c.stats.mu.Unlock()
	return v
}

// invalidateStats clears the stats cache so the next GetLocalStorageStats
// re-walks disk. Called after DeleteLocalVersion / ApplyRetentionNow
// success so the user sees their own action reflected immediately
// (design-log/045 §OQ3).
func (c *ControlService) invalidateStats() {
	c.stats.mu.Lock()
	c.stats.hasVal = false
	c.stats.mu.Unlock()
}
