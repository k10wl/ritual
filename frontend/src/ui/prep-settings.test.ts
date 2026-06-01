import { html } from "lit";
import { fixture, expect } from "@open-wc/testing";
import "./prep-settings";
import type { PrepSettingsEl, PrepSettingsChangeDetail } from "./prep-settings";

async function mount(): Promise<PrepSettingsEl> {
    const el = await fixture<PrepSettingsEl>(html`<prep-settings></prep-settings>`);
    await el.updateComplete;
    return el;
}

function skipSyncBox(el: PrepSettingsEl): HTMLInputElement {
    return el.shadowRoot!.querySelector(".skip-sync input") as HTMLInputElement;
}

describe("prep-settings — skip sync this session (design-log/036)", () => {
    it("defaults OFF on mount", async () => {
        const el = await mount();
        expect(el.skipSyncEnabled()).to.equal(false);
        expect(skipSyncBox(el).checked).to.equal(false);
    });

    it("is transient — a fresh mount resets it OFF", async () => {
        const a = await mount();
        skipSyncBox(a).click();
        await a.updateComplete;
        expect(a.skipSyncEnabled()).to.equal(true);
        // A brand-new element (a new "session") starts OFF again.
        const b = await mount();
        expect(b.skipSyncEnabled()).to.equal(false);
    });

    it("carries skipSync on the change detail", async () => {
        const el = await mount();
        let detail: PrepSettingsChangeDetail | null = null;
        el.addEventListener("change", (e) => {
            detail = (e as CustomEvent<PrepSettingsChangeDetail>).detail;
        });
        skipSyncBox(el).click();
        await el.updateComplete;
        expect(detail).to.not.equal(null);
        expect(detail!.skipSync).to.equal(true);
    });

    it("is not part of the persisted port/memory payload (read())", async () => {
        const el = await mount();
        skipSyncBox(el).click();
        await el.updateComplete;
        const settings = el.read();
        expect(settings).to.deep.equal({ port: 25565, memoryMB: 4096 });
        expect(Object.keys(settings ?? {})).to.not.include("skipSync");
    });
});
