import { html } from "lit";
import { fixture, expect } from "@open-wc/testing";
import "./advanced-view";
import type { AdvancedView } from "./advanced-view";

describe("advanced-view", () => {
    it("renders three flat sections — Server, Sync, Updates — with no menu nesting", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        const labels = [...el.shadowRoot!.querySelectorAll(".label")].map((l) =>
            l.textContent!.trim(),
        );
        expect(labels).to.deep.equal(["Server", "Sync", "Updates"]);
        expect(el.shadowRoot!.querySelector("prep-settings")).to.exist;
        expect(el.shadowRoot!.querySelector("sync-view")).to.exist;
    });

    it("passes config down to prep-settings", async () => {
        const el = await fixture<AdvancedView>(
            html`<advanced-view .config=${{ port: 30000, memoryMB: 8192 }}></advanced-view>`,
        );
        const prep = el.shadowRoot!.querySelector("prep-settings") as HTMLElement & {
            config: { port: number };
        };
        expect(prep.config.port).to.equal(30000);
    });

    it("passes the check probe down to sync-view", async () => {
        const probe = async () => ({ behind: true, ahead: false });
        const el = await fixture<AdvancedView>(
            html`<advanced-view .check=${probe}></advanced-view>`,
        );
        const sync = el.shadowRoot!.querySelector("sync-view") as HTMLElement & {
            check: unknown;
        };
        expect(sync.check).to.equal(probe);
    });

    it("lets child sync events bubble through to the host", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        let bubbled = false;
        el.addEventListener("sync", () => (bubbled = true));
        el.shadowRoot!
            .querySelector("sync-view")!
            .dispatchEvent(new CustomEvent("sync", { detail: { direction: "download" }, bubbles: true, composed: true }));
        expect(bubbled).to.equal(true);
    });

    it("disables Check for update unless canUpdate (dial idle) — design-log/037 §Q4", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        const btn = el.shadowRoot!.querySelector("rune-button") as HTMLElement & { disabled: boolean };
        expect(btn.disabled).to.equal(true, "default (not idle) keeps the update check gated");

        el.canUpdate = true;
        await el.updateComplete;
        expect(btn.disabled).to.equal(false, "idle enables the manual check");
    });

    it("emits a checkupdate event when the update button is pressed", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view .canUpdate=${true}></advanced-view>`);
        let fired = false;
        el.addEventListener("checkupdate", () => (fired = true));
        el.shadowRoot!.querySelector("rune-button")!.dispatchEvent(
            new CustomEvent("press", { bubbles: true, composed: true }),
        );
        expect(fired).to.equal(true);
    });
});
