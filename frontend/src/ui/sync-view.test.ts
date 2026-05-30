import { html } from "lit";
import { fixture, expect, oneEvent, aTimeout } from "@open-wc/testing";
import "./sync-view";
import type { SyncConfirmDetail, SyncVerdict, SyncView } from "./sync-view";

const mount = (check: () => Promise<SyncVerdict>) =>
    fixture<SyncView>(html`<sync-view .check=${check}></sync-view>`);

const button = (el: SyncView, text: string): HTMLElement | undefined =>
    [...el.shadowRoot!.querySelectorAll("rune-button")].find(
        (b) => b.textContent!.trim().includes(text),
    ) as HTMLElement | undefined;

const press = (b: HTMLElement) =>
    b.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true }));

const bodyText = (el: SyncView) => el.shadowRoot!.textContent!.replace(/\s+/g, " ").trim();

// The verdict line is a <rune-decoder>; its text lives in the `.text` property
// (its own shadow renders animated cells), not in sync-view's textContent.
const status = (el: SyncView) =>
    (el.shadowRoot!.querySelector("rune-decoder") as (HTMLElement & { text: string }) | null)?.text ?? "";

describe("sync-view", () => {
    it("starts unchecked with a Check action and no verdict", async () => {
        const el = await mount(async () => ({ behind: false, ahead: false }));
        expect(button(el, "Check remote")).to.exist;
        expect(bodyText(el)).to.not.contain("up to date");
    });

    it("auto → probes on mount, no button press needed (Advanced-transition recheck)", async () => {
        const el = await fixture<SyncView>(
            html`<sync-view auto .check=${async () => ({ behind: true, ahead: false })}></sync-view>`,
        );
        await aTimeout(0);
        await el.updateComplete;
        expect(status(el)).to.contain("A newer world is waiting");
        expect(button(el, "Check remote")).to.not.exist; // skipped the unchecked prompt
    });

    it("behind → offers Download", async () => {
        const el = await mount(async () => ({ behind: true, ahead: false }));
        press(button(el, "Check remote")!);
        await aTimeout(0);
        await el.updateComplete;
        expect(status(el)).to.contain("A newer world is waiting");
        expect(button(el, "Download")).to.exist;
        expect(button(el, "Upload")).to.not.exist;
    });

    it("ahead → offers Upload", async () => {
        const el = await mount(async () => ({ behind: false, ahead: true }));
        press(button(el, "Check remote")!);
        await aTimeout(0);
        await el.updateComplete;
        expect(status(el)).to.contain("local changes to publish");
        expect(button(el, "Upload")).to.exist;
        expect(button(el, "Download")).to.not.exist;
    });

    it("in sync → up-to-date, no action offered", async () => {
        const el = await mount(async () => ({ behind: false, ahead: false }));
        press(button(el, "Check remote")!);
        await aTimeout(0);
        await el.updateComplete;
        expect(status(el)).to.contain("Everything's up to date");
        expect(button(el, "Download")).to.not.exist;
        expect(button(el, "Upload")).to.not.exist;
    });

    it("probe rejects → couldn't reach, with retry", async () => {
        const el = await mount(() => Promise.reject(new Error("offline")));
        press(button(el, "Check remote")!);
        await aTimeout(0);
        await el.updateComplete;
        expect(status(el)).to.contain("Couldn't reach the remote");
        expect(button(el, "Try again")).to.exist;
    });

    it("Download is a two-step inline confirm — no event until confirmed", async () => {
        const el = await mount(async () => ({ behind: true, ahead: false }));
        press(button(el, "Check remote")!);
        await aTimeout(0);
        await el.updateComplete;

        let fired = false;
        el.addEventListener("sync", () => (fired = true));
        press(button(el, "Download")!); // first tap = reveal confirm only
        await el.updateComplete;
        expect(fired).to.equal(false);
        expect(bodyText(el)).to.contain("replaces your local copy");

        setTimeout(() => press(button(el, "Download")!)); // confirm
        const ev = await oneEvent(el, "sync");
        expect((ev.detail as SyncConfirmDetail).direction).to.equal("download");
    });

    it("Cancel backs out of the confirm without firing", async () => {
        const el = await mount(async () => ({ behind: true, ahead: false }));
        press(button(el, "Check remote")!);
        await aTimeout(0);
        await el.updateComplete;
        press(button(el, "Download")!);
        await el.updateComplete;

        let fired = false;
        el.addEventListener("sync", () => (fired = true));
        press(button(el, "Cancel")!);
        await el.updateComplete;
        expect(fired).to.equal(false);
        expect(button(el, "Download")).to.exist; // back to the verdict
    });
});
