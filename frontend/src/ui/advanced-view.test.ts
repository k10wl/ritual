import { html } from "lit";
import { fixture, expect } from "@open-wc/testing";
import "./advanced-view";
import type { AdvancedView } from "./advanced-view";

describe("advanced-view", () => {
    it("renders five flat sections — Server, Sync, Versions, Retention, Updates — with no menu nesting", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        const labels = [...el.shadowRoot!.querySelectorAll(".label")].map((l) =>
            l.textContent!.trim(),
        );
        expect(labels).to.deep.equal(["Server", "Sync", "Versions", "Retention", "Updates"]);
        expect(el.shadowRoot!.querySelector("prep-settings")).to.exist;
        expect(el.shadowRoot!.querySelector("sync-view")).to.exist;
        expect(el.shadowRoot!.querySelector("versions-view")).to.exist;
        expect(el.shadowRoot!.querySelector("retention-rules")).to.exist;
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

    it("passes the version listing + dirty flag down to versions-view (design-log/038)", async () => {
        const list = async () => [];
        const el = await fixture<AdvancedView>(
            html`<advanced-view .versions=${list} ?dirty=${true}></advanced-view>`,
        );
        const v = el.shadowRoot!.querySelector("versions-view") as HTMLElement & {
            list: unknown;
            dirty: boolean;
        };
        expect(v.list).to.equal(list);
        expect(v.dirty).to.equal(true);
    });

    it("re-emits retention-rules `change` as a distinct `retentionchange` (no collision with prep `change`)", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        let prepChange = false;
        let retentionChange: unknown = null;
        el.addEventListener("change", () => (prepChange = true));
        el.addEventListener("retentionchange", (e) => (retentionChange = (e as CustomEvent).detail));

        const detail = {
            local: { keepLast: 1, keepDaily: 0, keepWeekly: 0, keepMonthly: 0 },
            remote: { keepLast: 2, keepDaily: 0, keepWeekly: 0, keepMonthly: 0 },
        };
        el.shadowRoot!
            .querySelector("retention-rules")!
            .dispatchEvent(new CustomEvent("change", { detail, bubbles: true, composed: true }));

        expect(retentionChange).to.deep.equal(detail, "retention edits surface as retentionchange");
        expect(prepChange).to.equal(false, "a retention edit must not masquerade as a prep-settings change");
    });

    it("lets child restore events bubble through to the host", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        let bubbled = false;
        el.addEventListener("restore", () => (bubbled = true));
        el.shadowRoot!
            .querySelector("versions-view")!
            .dispatchEvent(new CustomEvent("restore", { detail: { refID: "x" }, bubbles: true, composed: true }));
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
