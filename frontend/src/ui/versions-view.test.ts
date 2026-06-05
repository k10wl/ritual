import { html } from "lit";
import { fixture, expect, oneEvent, aTimeout } from "@open-wc/testing";
import "./versions-view";
import type {
    DeleteConfirmDetail,
    LocalStorageStatsLike,
    RestoreConfirmDetail,
    VersionRow,
    VersionScope,
    VersionsView,
} from "./versions-view";

const SAMPLE: VersionRow[] = [
    { id: "2026-06-04T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 4, 9, 30), files: 128, sizeBytes: 734_003_200, isHead: true, isLoaded: true, source: "local" },
    { id: "2026-06-03T21-05-00.000Z", unixMs: Date.UTC(2026, 5, 3, 21, 5), files: 126, sizeBytes: 720_000_000, isHead: false, isLoaded: false, source: "local" },
    { id: "2026-05-12T12-00-00.000Z", unixMs: Date.UTC(2026, 4, 12, 12, 0), files: 1, sizeBytes: 4096, isHead: false, isLoaded: false, source: "local" },
];

// Post-Restore listing: the workdir reflects an older ref (design-log/044).
// HEAD stays the newest; "current" follows the workdir, not HEAD.
const RESTORED_SAMPLE: VersionRow[] = [
    { id: "2026-06-04T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 4, 9, 30), files: 128, sizeBytes: 734_003_200, isHead: true, isLoaded: false, source: "local" },
    { id: "2026-06-03T21-05-00.000Z", unixMs: Date.UTC(2026, 5, 3, 21, 5), files: 126, sizeBytes: 720_000_000, isHead: false, isLoaded: true, source: "local" },
    { id: "2026-05-12T12-00-00.000Z", unixMs: Date.UTC(2026, 4, 12, 12, 0), files: 1, sizeBytes: 4096, isHead: false, isLoaded: false, source: "local" },
];

const ZERO_STATS: LocalStorageStatsLike = { bytesOnDisk: 0, objectCount: 0 };

const mount = (
    listOrFn: (() => Promise<VersionRow[]>) | ((scope: VersionScope) => Promise<VersionRow[]>),
    dirty = false,
    stats: () => Promise<LocalStorageStatsLike> = async () => ZERO_STATS,
) => {
    // Adapt the old single-arg list shape used by the existing tests to the
    // new scope-aware signature so the rest of the tests keep working as-is.
    const list = (_scope: VersionScope) =>
        (listOrFn as (s?: VersionScope) => Promise<VersionRow[]>)(_scope);
    return fixture<VersionsView>(
        html`<versions-view ?dirty=${dirty} .list=${list} .stats=${stats}></versions-view>`,
    );
};

const rows = (el: VersionsView) => [...el.shadowRoot!.querySelectorAll("rune-row")] as HTMLElement[];

const button = (el: VersionsView, text: string): HTMLElement | undefined =>
    [...el.shadowRoot!.querySelectorAll("rune-button")].find((b) =>
        b.textContent!.trim().includes(text),
    ) as HTMLElement | undefined;

const press = (b: HTMLElement) => b.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true }));

// Status copy lives in the <rune-decoder>'s `.text` property (it renders
// animated cells into its own shadow), not in versions-view's textContent.
const status = (el: VersionsView) =>
    (el.shadowRoot!.querySelector("rune-decoder") as (HTMLElement & { text: string }) | null)?.text ?? "";

const settle = async (el: VersionsView) => {
    await aTimeout(0);
    await el.updateComplete;
};

describe("versions-view", () => {
    it("lists versions newest-first with the current one badged and non-pressable", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        const r = rows(el);
        expect(r.length).to.equal(3);
        // HEAD row carries the badge and is not a restore target.
        expect(r[0].textContent).to.contain("current");
        expect(r[0].hasAttribute("pressable")).to.equal(false);
        // Older rows are pressable.
        expect(r[1].hasAttribute("pressable")).to.equal(true);
    });

    it("formats file count and size into the row meta", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        const text = el.shadowRoot!.textContent!.replace(/\s+/g, " ");
        expect(text).to.contain("128 files");
        expect(text).to.contain("1 file ·"); // singular for the 1-file ref
        expect(text).to.contain("MB");
    });

    it("empty history → no-versions copy, no rows", async () => {
        const el = await mount(async () => []);
        await settle(el);
        expect(rows(el).length).to.equal(0);
        expect(status(el)).to.contain("No earlier versions");
    });

    it("listing rejects → couldn't-load with retry", async () => {
        const el = await mount(() => Promise.reject(new Error("offline")));
        await settle(el);
        expect(status(el)).to.contain("Couldn't load versions");
        expect(button(el, "Try again")).to.exist;
    });

    it("restore is a two-step inline confirm — no event until confirmed", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);

        let fired = false;
        el.addEventListener("restore", () => (fired = true));
        press(rows(el)[1]); // first tap = reveal confirm only
        await el.updateComplete;
        expect(fired).to.equal(false);
        expect(el.shadowRoot!.textContent).to.contain("Bring back the world");

        setTimeout(() => press(button(el, "Restore")!)); // confirm
        const ev = await oneEvent(el, "restore");
        expect((ev.detail as RestoreConfirmDetail).refID).to.equal("2026-06-03T21-05-00.000Z");
    });

    it("Cancel backs out of the confirm without firing", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        press(rows(el)[1]);
        await el.updateComplete;

        let fired = false;
        el.addEventListener("restore", () => (fired = true));
        press(button(el, "Cancel")!);
        await el.updateComplete;
        expect(fired).to.equal(false);
        expect(rows(el).length).to.equal(3); // back to the list
    });

    it("dirty → restore confirm shows the Publish-first nudge that emits publishfirst", async () => {
        const el = await mount(async () => SAMPLE, true);
        await settle(el);
        press(rows(el)[1]);
        await el.updateComplete;
        expect(el.shadowRoot!.querySelector(".warn"), "unsaved-changes warning").to.exist;

        setTimeout(() => press(button(el, "Publish first")!));
        await oneEvent(el, "publishfirst");
    });

    it("post-Restore: the 'current' badge follows isLoaded, not HEAD (design-log/044)", async () => {
        const el = await mount(async () => RESTORED_SAMPLE);
        await settle(el);
        const r = rows(el);
        // HEAD is the newest row but the workdir reflects the second one.
        expect(r[0].textContent).to.not.contain("current");
        expect(r[0].hasAttribute("pressable")).to.equal(true, "HEAD is pressable when it isn't loaded");
        expect(r[1].textContent).to.contain("current");
        expect(r[1].hasAttribute("pressable")).to.equal(false, "loaded row is not a restore target");
    });

    it("not dirty → no Publish-first nudge in the confirm", async () => {
        const el = await mount(async () => SAMPLE, false);
        await settle(el);
        press(rows(el)[1]);
        await el.updateComplete;
        expect(el.shadowRoot!.querySelector(".warn")).to.not.exist;
        expect(button(el, "Publish first")).to.not.exist;
    });

    // ── design-log/045 ──────────────────────────────────────────────────────

    it("defaults to the Local tab and exposes a Local · Remote segmented switch", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        const seg = el.shadowRoot!.querySelector("rune-segmented") as HTMLElement & {
            value: string;
        };
        expect(seg).to.exist;
        expect(seg.value).to.equal("local");
    });

    it("switching the tab re-runs the listing with the new scope (design-log/045 §B)", async () => {
        const seen: string[] = [];
        const list = async (scope: VersionScope) => {
            seen.push(scope);
            return SAMPLE;
        };
        const el = await mount(list);
        await settle(el);
        expect(seen).to.deep.equal(["local"]);

        // Simulate a Remote tab pick.
        el.shadowRoot!.querySelector("rune-segmented")!.dispatchEvent(
            new CustomEvent("change", {
                detail: { value: "remote" },
                bubbles: true,
                composed: true,
            }),
        );
        await settle(el);
        expect(seen).to.deep.equal(["local", "remote"]);
    });

    it("Local tab shows the × delete affordance on every row (design-log/045 §A)", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        const dels = el.shadowRoot!.querySelectorAll(".del");
        // Every row gets a delete affordance — including the loaded one
        // (design-log/045 §Q2: user can delete anything; confirm copy
        // disambiguates).
        expect(dels.length).to.equal(SAMPLE.length);
    });

    it("Remote tab also shows the × delete affordance on every row (045 post-ship extension, user 2026-06-05)", async () => {
        // The original 045 §Q4 hid the × on Remote (v1 read-only canonical
        // history). The user lifted that gate — they own the store and want
        // to be able to delete anything. The confirm copy on Remote spells
        // out the sharp edge instead.
        const el = await mount(async () => SAMPLE);
        await settle(el);
        el.shadowRoot!.querySelector("rune-segmented")!.dispatchEvent(
            new CustomEvent("change", {
                detail: { value: "remote" },
                bubbles: true,
                composed: true,
            }),
        );
        await settle(el);
        expect(el.shadowRoot!.querySelectorAll(".del").length).to.equal(SAMPLE.length);
    });

    it("Remote-tab delete confirm warns about canonical history loss + emits scope:'remote'", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        el.shadowRoot!.querySelector("rune-segmented")!.dispatchEvent(
            new CustomEvent("change", {
                detail: { value: "remote" },
                bubbles: true,
                composed: true,
            }),
        );
        await settle(el);

        const dels = el.shadowRoot!.querySelectorAll(".del") as NodeListOf<HTMLElement>;
        // Press × on a non-HEAD row (older history entry).
        dels[2].click();
        await el.updateComplete;
        expect(el.shadowRoot!.textContent).to.contain("canonical history");
        expect(el.shadowRoot!.textContent).to.contain("Local caches");

        setTimeout(() => press(button(el, "Delete")!));
        const ev = await oneEvent(el, "delete");
        const d = ev.detail as DeleteConfirmDetail;
        expect(d.refID).to.equal("2026-05-12T12-00-00.000Z");
        expect(d.scope).to.equal("remote", "scope payload routes the host to deleteRemoteVersion");
    });

    it("Remote-tab delete on HEAD warns it's the latest canonical version", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        el.shadowRoot!.querySelector("rune-segmented")!.dispatchEvent(
            new CustomEvent("change", {
                detail: { value: "remote" },
                bubbles: true,
                composed: true,
            }),
        );
        await settle(el);

        const dels = el.shadowRoot!.querySelectorAll(".del") as NodeListOf<HTMLElement>;
        dels[0].click(); // HEAD on remote
        await el.updateComplete;
        expect(el.shadowRoot!.textContent).to.contain("latest canonical version");
        expect(el.shadowRoot!.textContent).to.contain("new HEAD");
    });

    it("Local-tab delete carries scope:'local' in the event detail", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        const dels = el.shadowRoot!.querySelectorAll(".del") as NodeListOf<HTMLElement>;
        dels[2].click();
        await el.updateComplete;

        setTimeout(() => press(button(el, "Delete")!));
        const ev = await oneEvent(el, "delete");
        const d = ev.detail as DeleteConfirmDetail;
        expect(d.scope).to.equal("local", "Local tab × emits scope:'local'");
    });

    it("delete is a two-step inline confirm — no event until Delete pressed", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);

        let fired = false;
        el.addEventListener("delete", () => (fired = true));
        const dels = el.shadowRoot!.querySelectorAll(".del") as NodeListOf<HTMLElement>;
        dels[2].click(); // click the × on the oldest row
        await el.updateComplete;
        expect(fired).to.equal(false);
        expect(el.shadowRoot!.textContent).to.contain("Delete this local copy");

        setTimeout(() => press(button(el, "Delete")!));
        const ev = await oneEvent(el, "delete");
        expect((ev.detail as DeleteConfirmDetail).refID).to.equal("2026-05-12T12-00-00.000Z");
    });

    it("clicking × on a non-loaded row does not also fire restore (stopPropagation)", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        let restoreFired = false;
        el.addEventListener("restore", () => (restoreFired = true));
        const dels = el.shadowRoot!.querySelectorAll(".del") as NodeListOf<HTMLElement>;
        dels[1].click();
        await el.updateComplete;
        expect(restoreFired).to.equal(false);
        // Confirm is the delete confirm, not restore.
        expect(el.shadowRoot!.textContent).to.contain("Delete this local copy");
    });

    it("delete confirm on the loaded HEAD warns about losing the anchor (design-log/045 §Q2)", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        const dels = el.shadowRoot!.querySelectorAll(".del") as NodeListOf<HTMLElement>;
        dels[0].click(); // loaded + HEAD
        await el.updateComplete;
        expect(el.shadowRoot!.textContent).to.contain("This is your current version");
        expect(el.shadowRoot!.textContent).to.contain("loses the ref it was anchored to");
    });

    it("delete confirm on the loaded non-HEAD warns about dirty workdir (post-Restore)", async () => {
        const el = await mount(async () => RESTORED_SAMPLE);
        await settle(el);
        const dels = el.shadowRoot!.querySelectorAll(".del") as NodeListOf<HTMLElement>;
        // RESTORED_SAMPLE: row[1] is loaded but NOT HEAD.
        dels[1].click();
        await el.updateComplete;
        expect(el.shadowRoot!.textContent).to.contain("You're currently on this older version");
        expect(el.shadowRoot!.textContent).to.contain("read as dirty");
    });

    it("Local tab renders the on-disk header when stats resolve (design-log/045 §E)", async () => {
        const stats = async (): Promise<LocalStorageStatsLike> => ({
            bytesOnDisk: 1_250_000_000,
            objectCount: 4321,
        });
        const el = await mount(async () => SAMPLE, false, stats);
        await settle(el);
        // Stats fetch is fire-and-forget — give it a microtask to land.
        await aTimeout(0);
        await el.updateComplete;
        const headline = el.shadowRoot!.querySelector(".header .headline") as HTMLElement & {
            text: string;
        } | null;
        expect(headline?.text).to.contain("3 versions");
        expect(headline?.text).to.contain("on disk");
    });

    it("Remote tab does not fetch local stats", async () => {
        let statsCalls = 0;
        const stats = async (): Promise<LocalStorageStatsLike> => {
            statsCalls++;
            return ZERO_STATS;
        };
        const el = await mount(async () => SAMPLE, false, stats);
        await settle(el);
        await aTimeout(0);
        // Local mount loads stats once.
        expect(statsCalls).to.equal(1);
        el.shadowRoot!.querySelector("rune-segmented")!.dispatchEvent(
            new CustomEvent("change", {
                detail: { value: "remote" },
                bubbles: true,
                composed: true,
            }),
        );
        await settle(el);
        await aTimeout(0);
        // Remote tab does not re-fetch stats.
        expect(statsCalls).to.equal(1);
    });

    it("rapid tab toggles do not let a stale list overwrite the fresh tab (backpressure)", async () => {
        // Reproduces user bug 2026-06-05 #1: without an epoch guard, a slow
        // Remote response could land after the user is on Local and stomp
        // Local rows. The newer load's epoch must win.
        const LOCAL_ROWS: VersionRow[] = [
            { id: "2026-06-04T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 4, 9, 30), files: 1, sizeBytes: 100, isHead: true, isLoaded: true, source: "local" },
        ];
        const REMOTE_ROWS: VersionRow[] = [
            { id: "2026-06-04T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 4, 9, 30), files: 1, sizeBytes: 100, isHead: true, isLoaded: true, source: "remote" },
            { id: "2026-06-03T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 3, 9, 30), files: 1, sizeBytes: 100, isHead: false, isLoaded: false, source: "remote" },
        ];
        // Hold the Remote resolver — we'll resolve it manually AFTER switching
        // back to Local, so the stale response lands last.
        let resolveRemote!: (v: VersionRow[]) => void;
        const list = (scope: VersionScope) => {
            if (scope === "remote") {
                return new Promise<VersionRow[]>((res) => {
                    resolveRemote = res;
                });
            }
            return Promise.resolve(LOCAL_ROWS);
        };
        const el = await mount(list);
        await settle(el); // initial Local load lands
        expect(rows(el).length).to.equal(1);

        // Switch to Remote → fires the (held) list("remote")
        el.shadowRoot!.querySelector("rune-segmented")!.dispatchEvent(
            new CustomEvent("change", { detail: { value: "remote" }, bubbles: true, composed: true }),
        );
        await el.updateComplete;

        // Switch BACK to Local while Remote is still pending → bumps epoch.
        el.shadowRoot!.querySelector("rune-segmented")!.dispatchEvent(
            new CustomEvent("change", { detail: { value: "local" }, bubbles: true, composed: true }),
        );
        await settle(el);
        expect(rows(el).length).to.equal(1, "Local rows must be visible after the second switch");

        // Now the stale Remote response finally arrives. The epoch guard must
        // drop it on the floor.
        resolveRemote(REMOTE_ROWS);
        await aTimeout(0);
        await el.updateComplete;
        expect(rows(el).length).to.equal(1, "stale Remote payload must NOT stomp the fresh Local rows");
    });

    it("delete button is a real button sibling of the row, not nested inside it (per user 2026-06-05)", async () => {
        const el = await mount(async () => SAMPLE);
        await settle(el);
        const dels = [...el.shadowRoot!.querySelectorAll(".del")] as HTMLElement[];
        // Each × must be a real <button>, parented to .row-pair, and a sibling
        // (not a descendant) of the rune-row in that pair.
        for (const del of dels) {
            expect(del.tagName).to.equal("BUTTON");
            const pair = del.parentElement;
            expect(pair?.classList.contains("row-pair")).to.equal(true);
            const row = pair?.querySelector("rune-row");
            expect(row).to.exist;
            expect(row?.contains(del)).to.equal(false, "the × must NOT be a descendant of rune-row (no nested buttons)");
        }
    });

    it("dedup hint appears only when the logical sum dwarfs disk usage", async () => {
        // SAMPLE rows sum to ~1.45 GB logical. Set bytesOnDisk well under that
        // so ratio > 1.5×.
        const stats = async (): Promise<LocalStorageStatsLike> => ({
            bytesOnDisk: 500_000_000,
            objectCount: 1000,
        });
        const el = await mount(async () => SAMPLE, false, stats);
        await settle(el);
        await aTimeout(0);
        await el.updateComplete;
        const hint = el.shadowRoot!.querySelector(".header .hint") as HTMLElement & {
            text: string;
        } | null;
        expect(hint?.text).to.contain("Shared content");
    });
});
