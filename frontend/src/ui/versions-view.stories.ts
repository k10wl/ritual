import { html } from "lit";
import "./versions-view";
import type { LocalStorageStatsLike, VersionRow, VersionScope } from "./versions-view";

export default {
    title: "Components / Versions View",
    component: "versions-view",
    parameters: {
        docs: {
            description: {
                component:
                    "World-save rollback (design-log/038) + per-version delete + Local/Remote tabs + " +
                    "total-on-disk header (design-log/045). Lists historical refs newest-first; " +
                    "tapping an older row reveals an inline two-step Restore confirm, the × reveals " +
                    "a Delete confirm. The listing is injected via `.list(scope)`; stats via " +
                    "`.stats()`. Events: `restore`/`delete`/`publishfirst`.",
            },
        },
    },
};

const pane = (inner: unknown) => html`
    <div style="width:420px; background:var(--stone-deep); border:1px solid var(--stone-bevel);
                border-radius:var(--radius-lg); padding:var(--space-4);">
        ${inner}
    </div>
`;

// A synthetic history: HEAD plus three older snapshots of varying size.
// HEAD is what the workdir reflects in the steady state, so isLoaded == isHead
// here. (See the Restored story for the post-rollback split.)
const SAMPLE: VersionRow[] = [
    { id: "2026-06-04T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 4, 9, 30), files: 128, sizeBytes: 734_003_200, isHead: true, isLoaded: true, source: "local" },
    { id: "2026-06-03T21-05-00.000Z", unixMs: Date.UTC(2026, 5, 3, 21, 5), files: 126, sizeBytes: 720_000_000, isHead: false, isLoaded: false, source: "local" },
    { id: "2026-05-28T18-40-00.000Z", unixMs: Date.UTC(2026, 4, 28, 18, 40), files: 120, sizeBytes: 690_000_000, isHead: false, isLoaded: false, source: "local" },
    { id: "2026-05-12T12-00-00.000Z", unixMs: Date.UTC(2026, 4, 12, 12, 0), files: 1, sizeBytes: 4096, isHead: false, isLoaded: false, source: "local" },
];

// Post-Restore: the workdir reflects an older ref. HEAD stays the newest,
// but the "current" badge follows the workdir (design-log/044).
const RESTORED: VersionRow[] = [
    { id: "2026-06-04T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 4, 9, 30), files: 128, sizeBytes: 734_003_200, isHead: true, isLoaded: false, source: "local" },
    { id: "2026-06-03T21-05-00.000Z", unixMs: Date.UTC(2026, 5, 3, 21, 5), files: 126, sizeBytes: 720_000_000, isHead: false, isLoaded: true, source: "local" },
    { id: "2026-05-28T18-40-00.000Z", unixMs: Date.UTC(2026, 4, 28, 18, 40), files: 120, sizeBytes: 690_000_000, isHead: false, isLoaded: false, source: "local" },
    { id: "2026-05-12T12-00-00.000Z", unixMs: Date.UTC(2026, 4, 12, 12, 0), files: 1, sizeBytes: 4096, isHead: false, isLoaded: false, source: "local" },
];

const delayed = (v: VersionRow[]): ((scope: VersionScope) => Promise<VersionRow[]>) =>
    () => new Promise((res) => setTimeout(() => res(v), 400));

// Heavy dedup: four 700 MB-class versions sharing most blobs ⇒ ~900 MB on
// disk. Triggers the "Shared content keeps disk use small" hint.
const STATS_DEDUP: LocalStorageStatsLike = { bytesOnDisk: 900_000_000, objectCount: 4321 };

// Fresh single version, no dedup story to tell.
const STATS_ONE: LocalStorageStatsLike = { bytesOnDisk: 730_000_000, objectCount: 128 };

const log = (e: Event) =>
    // eslint-disable-next-line no-console
    console.log(e.type, "→", (e as CustomEvent).detail);

export const History = () =>
    pane(html`<versions-view
        .list=${delayed(SAMPLE)}
        .stats=${async () => STATS_DEDUP}
        @restore=${log}
        @delete=${log}
    ></versions-view>`);

// After a Restore the workdir reflects an older ref; "current" follows it,
// not HEAD (design-log/044). HEAD becomes pressable again.
export const Restored = () =>
    pane(html`<versions-view
        .list=${delayed(RESTORED)}
        .stats=${async () => STATS_DEDUP}
        @restore=${log}
        @delete=${log}
    ></versions-view>`);

// Dirty workdir → the restore confirm surfaces the "Publish first" nudge.
export const Dirty = () =>
    pane(html`<versions-view
        dirty
        .list=${delayed(SAMPLE)}
        .stats=${async () => STATS_DEDUP}
        @restore=${log}
        @publishfirst=${log}
        @delete=${log}
    ></versions-view>`);

// Single-version local store — no dedup hint, header reads "1 version · …".
export const OnlyCurrent = () =>
    pane(html`<versions-view
        .list=${delayed([SAMPLE[0]])}
        .stats=${async () => STATS_ONE}
        @restore=${log}
        @delete=${log}
    ></versions-view>`);

// Fresh install / empty history.
export const Empty = () =>
    pane(html`<versions-view
        .list=${delayed([])}
        .stats=${async () => ({ bytesOnDisk: 0, objectCount: 0 })}
        @restore=${log}
    ></versions-view>`);

// Listing rejects → graceful "couldn't load" with retry.
export const Error = () =>
    pane(html`<versions-view
        .list=${() => Promise.reject(new globalThis.Error("offline"))}
        @restore=${log}
    ></versions-view>`);
