import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./prep-settings";
import type { PrepSettingsEl, PrepSettingsSyncDetail } from "./prep-settings";
import type { RuneButton } from "./primitives/rune-button";
import type { RuneSheet } from "./primitives/rune-sheet";

// Sync gestures (design-log/031): Download/Upload rows open a rune-sheet
// confirm; the `sync` event fires only on confirm, never on cancel.
describe("prep-settings — sync gestures", () => {
    const syncButton = (el: PrepSettingsEl, label: string): RuneButton => {
        const buttons = [...el.shadowRoot!.querySelectorAll<RuneButton>(".sync rune-button")];
        const match = buttons.find((b) => b.textContent!.trim() === label);
        if (!match) throw new Error(`no sync button labelled ${label}`);
        return match;
    };

    const confirmSheet = (el: PrepSettingsEl): RuneSheet | null =>
        el.shadowRoot!.querySelector<RuneSheet>("rune-sheet");

    // rune-button emits `press` (not native click) from its inner shadow
    // <button>; clicking the host element is a no-op, so dispatch press.
    const press = (btn: RuneButton) =>
        btn.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true }));

    const footerButton = (sheet: RuneSheet, label: string): RuneButton =>
        [...sheet.querySelectorAll<RuneButton>('rune-button[slot="footer"]')]
            .find((b) => b.textContent!.trim() === label)!;

    it("shows no confirm dialog until a sync button is pressed", async () => {
        const el = await fixture<PrepSettingsEl>(html`<prep-settings></prep-settings>`);
        expect(confirmSheet(el)).to.equal(null);
    });

    it("opens a confirm dialog when Download is pressed, without firing sync", async () => {
        const el = await fixture<PrepSettingsEl>(html`<prep-settings></prep-settings>`);
        let fired = false;
        el.addEventListener("sync", () => { fired = true; });

        press(syncButton(el, "Download"));
        await el.updateComplete;

        expect(confirmSheet(el), "Download must open a confirm dialog").to.exist;
        expect(fired, "pressing Download must not fire sync — only confirming does").to.equal(false);
    });

    it("fires sync with direction=download when the dialog is confirmed", async () => {
        const el = await fixture<PrepSettingsEl>(html`<prep-settings></prep-settings>`);
        press(syncButton(el, "Download"));
        await el.updateComplete;

        const sync = oneEvent(el, "sync");
        press(footerButton(confirmSheet(el)!, "Download"));
        const e = await sync;

        expect((e.detail as PrepSettingsSyncDetail).direction).to.equal("download");
        await el.updateComplete;
        expect(confirmSheet(el), "confirming must close the dialog").to.equal(null);
    });

    it("fires sync with direction=upload when the upload dialog is confirmed", async () => {
        const el = await fixture<PrepSettingsEl>(html`<prep-settings></prep-settings>`);
        press(syncButton(el, "Upload"));
        await el.updateComplete;

        const sync = oneEvent(el, "sync");
        press(footerButton(confirmSheet(el)!, "Upload"));
        const e = await sync;

        expect((e.detail as PrepSettingsSyncDetail).direction).to.equal("upload");
    });

    it("does not fire sync when the dialog is cancelled", async () => {
        const el = await fixture<PrepSettingsEl>(html`<prep-settings></prep-settings>`);
        press(syncButton(el, "Upload"));
        await el.updateComplete;

        let fired = false;
        el.addEventListener("sync", () => { fired = true; });

        press(footerButton(confirmSheet(el)!, "Cancel"));
        await el.updateComplete;

        expect(fired, "cancelling the confirm must never fire sync").to.equal(false);
        expect(confirmSheet(el), "cancelling must close the dialog").to.equal(null);
    });
});
