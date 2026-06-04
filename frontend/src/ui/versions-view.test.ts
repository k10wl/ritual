import { html } from "lit";
import { fixture, expect, oneEvent, aTimeout } from "@open-wc/testing";
import "./versions-view";
import type { RestoreConfirmDetail, VersionRow, VersionsView } from "./versions-view";

const SAMPLE: VersionRow[] = [
    { id: "2026-06-04T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 4, 9, 30), files: 128, sizeBytes: 734_003_200, isHead: true, source: "remote" },
    { id: "2026-06-03T21-05-00.000Z", unixMs: Date.UTC(2026, 5, 3, 21, 5), files: 126, sizeBytes: 720_000_000, isHead: false, source: "remote" },
    { id: "2026-05-12T12-00-00.000Z", unixMs: Date.UTC(2026, 4, 12, 12, 0), files: 1, sizeBytes: 4096, isHead: false, source: "remote" },
];

const mount = (list: () => Promise<VersionRow[]>, dirty = false) =>
    fixture<VersionsView>(html`<versions-view ?dirty=${dirty} .list=${list}></versions-view>`);

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

    it("not dirty → no Publish-first nudge in the confirm", async () => {
        const el = await mount(async () => SAMPLE, false);
        await settle(el);
        press(rows(el)[1]);
        await el.updateComplete;
        expect(el.shadowRoot!.querySelector(".warn")).to.not.exist;
        expect(button(el, "Publish first")).to.not.exist;
    });
});
