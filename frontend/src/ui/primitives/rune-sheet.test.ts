import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-sheet";
import type { RuneSheet, RuneSheetCloseDetail } from "./rune-sheet";

describe("rune-sheet", () => {
    it("renders hidden by default", async () => {
        const el = await fixture<RuneSheet>(html`<rune-sheet heading="Hi">Body</rune-sheet>`);
        const dialog = el.shadowRoot!.querySelector("dialog")!;
        expect(dialog.open).to.equal(false);
        expect(el.open).to.equal(false);
    });

    it("opens via show()", async () => {
        const el = await fixture<RuneSheet>(html`<rune-sheet>Body</rune-sheet>`);
        const openEvent = oneEvent(el, "open");
        el.show();
        await el.updateComplete;
        await openEvent;
        expect(el.open).to.equal(true);
        const dialog = el.shadowRoot!.querySelector("dialog")!;
        expect(dialog.open).to.equal(true);
        // Tidy: close before fixture teardown so modal stack doesn't leak.
        el.close();
        await el.updateComplete;
    });

    it("closes via close() with explicit reason", async () => {
        const el = await fixture<RuneSheet>(html`<rune-sheet>Body</rune-sheet>`);
        let receivedReason: RuneSheetDismissReason | null = null;
        el.addEventListener("close", (e) => {
            receivedReason = (e as CustomEvent<RuneSheetCloseDetail>).detail.reason;
        });
        el.show();
        await el.updateComplete;
        el.close();
        await el.updateComplete;
        // The native dialog `close` event is microtask-queued — yield once.
        await new Promise((r) => setTimeout(r, 0));
        expect(el.open).to.equal(false);
        expect(receivedReason).to.equal("explicit");
    });

    it("renders heading slot from attribute", async () => {
        const el = await fixture<RuneSheet>(html`<rune-sheet heading="Title">Body</rune-sheet>`);
        await el.updateComplete;
        const header = el.shadowRoot!.querySelector("header")!;
        expect(header).to.exist;
        expect(header.textContent!.trim()).to.equal("Title");
    });

    it("omits footer element when no footer-slotted children", async () => {
        const el = await fixture<RuneSheet>(html`<rune-sheet>Body</rune-sheet>`);
        await el.updateComplete;
        const footer = el.shadowRoot!.querySelector("footer");
        expect(footer).to.equal(null);
    });
});
