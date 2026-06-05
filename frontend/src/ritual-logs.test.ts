import { html } from "lit";
import { fixture, expect } from "@open-wc/testing";
import "./ritual-logs";
import { seamOverlap, type RitualLogs } from "./ritual-logs";
import { Level, type ServerLog, type ServerLogBatch } from "./wails-api";

const mount = () => fixture<RitualLogs>(html`<ritual-logs></ritual-logs>`);

const out = (text: string): ServerLog => ({ ts: 1, kind: "out", level: Level.$zero, text });
const batch = (lines: ServerLog[], dropped = 0): ServerLogBatch => ({ lines, dropped });

// appendBatch is the imperative core (design-log/042); drive it directly rather
// than through the Wails event bridge, which is unavailable in the test browser.
// Rows land on a microtask (flush is microtask-batched), so drain it.
const feed = async (el: RitualLogs, b: ServerLogBatch) => {
    (el as unknown as { appendBatch(b: ServerLogBatch): void }).appendBatch(b);
    await Promise.resolve(); // let the queued flush microtask run
    await el.updateComplete;
};

const ol = (el: RitualLogs) => el.shadowRoot!.querySelector("ol")!;
const rows = (el: RitualLogs) => [...ol(el).querySelectorAll("li:not(.sentinel)")];
const sentinel = (el: RitualLogs) => el.shadowRoot!.querySelector(".sentinel")!;

describe("ritual-logs", () => {
    it("renders the empty-state copy before any output", async () => {
        const el = await mount();
        expect(el.shadowRoot!.textContent).to.contain("output appears here while the world is running");
        expect(rows(el)).to.have.lengthOf(0);
    });

    it("appends in order with the newest just above the bottom sentinel", async () => {
        const el = await mount();
        await feed(el, batch([out("a"), out("b"), out("c")]));
        // Normal top→bottom flow: oldest first, newest last before the sentinel.
        expect(rows(el).map((r) => r.textContent)).to.deep.equal(["a", "b", "c"]);
        expect(sentinel(el).previousElementSibling!.textContent).to.equal("c");
        expect(sentinel(el)).to.equal(ol(el).lastElementChild); // sentinel stays last
    });

    it("bounds the DOM at the 1024-row ring while following the tail, dropping the oldest", async () => {
        const el = await mount();
        const lines: ServerLog[] = [];
        for (let i = 0; i < 1100; i++) lines.push(out("L" + i));
        await feed(el, batch(lines)); // at the tail ⇒ trims to cap
        expect(rows(el)).to.have.lengthOf(1024);
        // 1100 - 1024 = 76 oldest dropped; newest survives at the bottom.
        expect(ol(el).firstElementChild!.textContent).to.equal("L76"); // oldest survivor
        expect(sentinel(el).previousElementSibling!.textContent).to.equal("L1099");
    });

    it("defers trimming while scrolled up, then catches up on return to the tail", async () => {
        const el = await mount();
        const seed: ServerLog[] = [];
        for (let i = 0; i < 1100; i++) seed.push(out("L" + i));
        await feed(el, batch(seed)); // fills + trims to cap, pinned at the tail
        expect(rows(el)).to.have.lengthOf(1024);

        const sc = ol(el);
        sc.scrollTop = 0; // scroll up to read back
        await feed(el, batch([out("x1"), out("x2")]));
        // No trim while scrolled up — the rows under the cursor must not reflow.
        expect(rows(el).length).to.be.greaterThan(1024);

        sc.scrollTop = sc.scrollHeight; // return to the tail
        await feed(el, batch([out("newest")]));
        expect(rows(el)).to.have.lengthOf(1024);
        expect(sentinel(el).previousElementSibling!.textContent).to.equal("newest");
    });

    it("renders an echoed command as a › input row", async () => {
        const el = await mount();
        await feed(el, batch([{ ts: 1, kind: "in", level: Level.$zero, text: "time set day" }]));
        const row = rows(el)[0];
        expect(row.classList.contains("row-input")).to.be.true;
        expect(row.textContent).to.equal("› time set day");
    });

    it("tints WARN/ERROR from MC's own tag, and a backend crash always", async () => {
        const el = await mount();
        await feed(el, batch([
            out("[14:00:00] [Server thread/WARN]: Can't keep up!"),
            out("[14:00:01] [Server thread/ERROR]: Failed to load chunk"),
            out("[14:00:02] [Server thread/INFO]: k10wl joined the game"),
            { ts: 1, kind: "out", level: Level.LevelError, text: "server crashed: exit status 1" },
        ]));
        const byText = (needle: string) => rows(el).find((r) => r.textContent!.includes(needle))!;
        expect(byText("WARN").classList.contains("lvl-warn")).to.be.true;
        expect(byText("ERROR").classList.contains("lvl-error")).to.be.true;
        expect(byText("joined").classList.contains("lvl-warn")).to.be.false;
        expect(byText("joined").classList.contains("lvl-error")).to.be.false;
        expect(byText("crashed").classList.contains("lvl-error")).to.be.true;
    });

    it("surfaces a dropped-lines gap row", async () => {
        const el = await mount();
        await feed(el, batch([out("after the gap")], 42));
        const gap = el.shadowRoot!.querySelector(".row-gap")!;
        expect(gap).to.exist;
        expect(gap.textContent).to.contain("42 lines dropped");
    });

    it("offers the Jump-to-latest pill only when scrolled up", async () => {
        const el = await mount();
        const pill = () => el.shadowRoot!.querySelector(".jump") as HTMLButtonElement;
        expect(pill().hidden).to.be.true; // pinned to the tail by default
        (el as unknown as { atBottom: boolean }).atBottom = false;
        await el.updateComplete;
        expect(pill().hidden).to.be.false;
    });
});

// seamOverlap drives the read↔live dedup at the backfill handoff (design-log/043
// §Q7): file tail and the first live lines are the same stdout, so the contiguous
// overlap is dropped by a plain text compare.
describe("ritual-logs seamOverlap", () => {
    it("returns 0 when the live tail does not overlap the backfill", () => {
        expect(seamOverlap(["a", "b", "c"].map(out), ["d", "e"].map(out))).to.equal(0);
    });

    it("detects a one-line overlap at the seam", () => {
        expect(seamOverlap(["a", "b", "c"].map(out), ["c", "d", "e"].map(out))).to.equal(1);
    });

    it("detects a multi-line overlap", () => {
        expect(seamOverlap(["x", "b", "c"].map(out), ["b", "c", "z"].map(out))).to.equal(2);
    });

    it("prefers the largest contiguous overlap", () => {
        expect(seamOverlap(["c", "b", "c"].map(out), ["b", "c", "n"].map(out))).to.equal(2);
    });

    it("handles empty inputs", () => {
        expect(seamOverlap([], ["a"].map(out))).to.equal(0);
        expect(seamOverlap(["a"].map(out), [])).to.equal(0);
    });
});
