import { html } from "lit";
import { fixture, expect } from "@open-wc/testing";
import "./advanced-view";
import type { AdvancedView } from "./advanced-view";

describe("advanced-view", () => {
    it("renders two flat sections — Server and Sync — with no menu nesting", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        const labels = [...el.shadowRoot!.querySelectorAll(".label")].map((l) =>
            l.textContent!.trim(),
        );
        expect(labels).to.deep.equal(["Server", "Sync"]);
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
});
