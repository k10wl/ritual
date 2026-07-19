package control

import (
	"context"
	"ritual/internal/core/domain"
	"sort"
	"strings"
	"time"
)

// versionsTimeout bounds ListVersions so a slow or offline remote can't hang
// the Versions screen — mirrors syncProbeTimeout. On timeout the method
// degrades to nil and the UI shows its empty/error state.
const versionsTimeout = 8 * time.Second

// Version is one historical ref surfaced to the Versions section in Advanced
// (design-log/038). It carries just enough to choose a restore target: when it
// was taken, how big it is, whether it is the current HEAD, and which store it
// came from. UnixMs is the parsed RefID so the frontend formats the date in the
// user's locale (render-purity friendly). Parent is read into the model for a
// future lineage view but is not rendered in v1.
//
// IsLoaded marks the ref whose blobs the workdir currently reflects
// (design-log/044). After a Restore that is the restored target, not HEAD;
// after a normal session/Publish it tracks the latest pushed ref. On a
// fresh install (settings.LoadedRefID empty) the lister falls back to the
// newest ref so the "current" badge is never silent.
type Version struct {
	ID        string `json:"id"`        // RefID timestamp (the restore target)
	UnixMs    int64  `json:"unixMs"`    // ID parsed to epoch millis, 0 if unparseable
	Parent    string `json:"parent"`    // parent RefID, "" for a root ref
	Files     int    `json:"files"`     // len(ref.Objects)
	SizeBytes int64  `json:"sizeBytes"` // Σ Object.Size (logical bytes)
	IsHead    bool   `json:"isHead"`    // the newest ref in this store
	IsLoaded  bool   `json:"isLoaded"`  // workdir currently reflects this ref (design-log/044)
	Source    string `json:"source"`    // "local" | "remote"
}

// VersionScope bundles the two reads ListVersions needs for one store: list the
// refs/ keyspace and load a single ref's metadata. Injected so control owns no
// storage/json wiring (the composition root supplies closures over each store).
type VersionScope struct {
	List    func(ctx context.Context, prefix string) ([]string, error)
	ReadRef RefReader
}

// VersionLister enumerates a store's refs with metadata, newest-first. scope is
// "local" or "remote"; a remote-listing failure degrades to local (design-log
// /038 §Q2) so an offline user can still roll back to cached versions.
type VersionLister func(ctx context.Context, scope string) ([]Version, error)

// LoadedIDFn returns the RefID the workdir currently reflects (design-log/044).
// Read fresh on every list so a Restore/Publish that landed between mounts is
// visible without re-wiring. Returns "" on a never-pulled install or a load
// error; the lister then falls back to flagging HEAD.
type LoadedIDFn func() domain.RefID

const refsPrefix = "refs/"

// NewVersionLister builds the lister over a local and a remote VersionScope.
// remote failures fall back to local; the returned Versions carry the Source
// that actually produced them so the UI can say which store it read.
// loadedID may be nil (treated as always returning ""); the lister then flags
// only IsHead and falls back to it for IsLoaded.
func NewVersionLister(local, remote VersionScope, loadedID LoadedIDFn) VersionLister {
	if loadedID == nil {
		loadedID = func() domain.RefID { return "" }
	}
	return func(ctx context.Context, scope string) ([]Version, error) {
		loaded := loadedID()
		if scope == "remote" {
			vs, err := listScope(ctx, remote, "remote", loaded)
			if err != nil {
				// Degrade to cached local history (§Q2). The relabelled Source
				// tells the UI it got local, not remote.
				return listScope(ctx, local, "local", loaded)
			}
			return vs, nil
		}
		return listScope(ctx, local, "local", loaded)
	}
}

// listScope lists refs/, parses ids newest-first, and reads each ref's
// metadata. Unparseable keys are skipped. The newest id is flagged IsHead.
// IsLoaded marks the id equal to loaded (design-log/044); when loaded is "",
// IsLoaded falls back to IsHead so a fresh install still shows "current".
// A ref whose body can't be read is included with zero counts rather than
// failing the whole listing — a half-written ref shouldn't hide the rest.
func listScope(ctx context.Context, src VersionScope, source string, loaded domain.RefID) ([]Version, error) {
	keys, err := src.List(ctx, refsPrefix)
	if err != nil {
		return nil, err
	}
	ids := parseRefIDs(keys)
	if len(ids) == 0 {
		return nil, nil
	}
	// Newest-first (RefID timestamps sort chronologically as strings).
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	head := ids[0]

	out := make([]Version, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		isHead := id == head
		isLoaded := loaded != "" && id == loaded
		if loaded == "" {
			isLoaded = isHead
		}
		v := Version{ID: string(id), IsHead: isHead, IsLoaded: isLoaded, Source: source}
		if t, perr := time.Parse(domain.RefIDFormat, string(id)); perr == nil {
			v.UnixMs = t.UnixMilli()
		}
		if ref, rerr := src.ReadRef(ctx, id); rerr == nil && ref != nil {
			v.Parent = string(ref.Parent)
			v.Files = len(ref.Objects)
			for _, o := range ref.Objects {
				v.SizeBytes += o.Size
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// parseRefIDs strips the "refs/" prefix + ".json" suffix from storage keys and
// keeps the ones that parse as a RefID timestamp; anything else (a stray key, a
// directory marker) is ignored.
func parseRefIDs(keys []string) []domain.RefID {
	ids := make([]domain.RefID, 0, len(keys))
	for _, k := range keys {
		if !strings.HasPrefix(k, refsPrefix) || !strings.HasSuffix(k, ".json") {
			continue
		}
		stem := strings.TrimSuffix(strings.TrimPrefix(k, refsPrefix), ".json")
		if _, err := time.Parse(domain.RefIDFormat, stem); err != nil {
			continue
		}
		ids = append(ids, domain.RefID(stem))
	}
	return ids
}
