import { html } from "lit";
import { fixture, expect } from "@open-wc/testing";
import "./advanced-view";
import type { AdvancedView } from "./advanced-view";

describe("advanced-view", () => {
    it("renders six flat sections — Server, Sync, Work folder, Versions, Retention, Updates — with no menu nesting", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        const labels = [...el.shadowRoot!.querySelectorAll(".label")].map((l) =>
            l.textContent!.trim(),
        );
        expect(labels).to.deep.equal([
            "Server",
            "Sync",
            "Work folder",
            "Versions",
            "Retention",
            "Updates",
        ]);
        expect(el.shadowRoot!.querySelector("prep-settings")).to.exist;
        expect(el.shadowRoot!.querySelector("sync-view")).to.exist;
        expect(el.shadowRoot!.querySelector("work-root")).to.exist;
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

    it("forwards versionStats down to versions-view (design-log/045 §E)", async () => {
        const stats = async () => ({ bytesOnDisk: 0, objectCount: 0 });
        const el = await fixture<AdvancedView>(
            html`<advanced-view .versionStats=${stats}></advanced-view>`,
        );
        const v = el.shadowRoot!.querySelector("versions-view") as HTMLElement & {
            stats: unknown;
        };
        expect(v.stats).to.equal(stats);
    });

    it("lets child delete events bubble through to the host (design-log/045 §A)", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        let bubbled = false;
        el.addEventListener("delete", () => (bubbled = true));
        el.shadowRoot!
            .querySelector("versions-view")!
            .dispatchEvent(
                new CustomEvent("delete", {
                    detail: { refID: "x" },
                    bubbles: true,
                    composed: true,
                }),
            );
        expect(bubbled).to.equal(true);
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

    it("switching the retention scope keeps the loaded rules (no all-zeros wipe) — design-log/045 post-ship", async () => {
        const rules = {
            local: { keepLast: 2, keepDaily: 0, keepWeekly: 0, keepMonthly: 0 },
            remote: { keepLast: 3, keepDaily: 1, keepWeekly: 1, keepMonthly: 6 },
        };
        const el = await fixture<AdvancedView>(
            html`<advanced-view .loadRules=${async () => rules}></advanced-view>`,
        );
        // firstUpdated → loadRules resolves a microtask later; flush it.
        await new Promise((r) => setTimeout(r, 0));
        await el.updateComplete;

        const ret = el.shadowRoot!.querySelector("retention-rules") as HTMLElement & {
            local: typeof rules.local;
            remote: typeof rules.remote;
        };
        expect(ret.local).to.deep.equal(rules.local, "baseline loaded before the flip");
        expect(ret.remote).to.deep.equal(rules.remote);

        // Flip scope to Remote via the real inner primitive (composed change).
        ret.shadowRoot!
            .querySelector("rune-segmented")!
            .dispatchEvent(new CustomEvent("change", { detail: { value: "remote" }, bubbles: true, composed: true }));
        await el.updateComplete;
        await ret.updateComplete;

        // Baseline survives the flip — was being nulled to {undefined} → zeros.
        expect(ret.local).to.deep.equal(rules.local, "scope flip must not clobber the host baseline");
        expect(ret.remote).to.deep.equal(rules.remote);
        const stepperValues = [...ret.shadowRoot!.querySelectorAll("rune-stepper")].map(
            (s) => (s as HTMLElement & { value: number }).value,
        );
        expect(stepperValues).to.deep.equal([3, 1, 1, 6], "remote tab shows the saved rules, not zeros");
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

    it("forwards skipSync down to prep-settings (design-log/044 §Phase C)", async () => {
        const el = await fixture<AdvancedView>(
            html`<advanced-view ?skipSync=${true}></advanced-view>`,
        );
        const prep = el.shadowRoot!.querySelector("prep-settings") as HTMLElement & {
            skipSync: boolean;
        };
        expect(prep.skipSync).to.equal(true);
    });

    it("calls openControlFolder when the Open app folder button is pressed", async () => {
        let called = false;
        const el = await fixture<AdvancedView>(
            html`<advanced-view .openControlFolder=${async () => {
                called = true;
            }}></advanced-view>`,
        );
        const btn = [...el.shadowRoot!.querySelectorAll("rune-button")].find((b) =>
            b.textContent?.includes("Open app folder"),
        )!;
        btn.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true }));
        expect(called).to.equal(true);
    });

    function checkUpdateButton(el: AdvancedView): (HTMLElement & { disabled: boolean }) | undefined {
        return [...el.shadowRoot!.querySelectorAll("rune-button")].find((b) =>
            b.textContent?.includes("Check for update"),
        ) as (HTMLElement & { disabled: boolean }) | undefined;
    }

    it("disables Check for update unless canUpdate (dial idle) — design-log/037 §Q4", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view></advanced-view>`);
        const btn = checkUpdateButton(el)!;
        expect(btn.disabled).to.equal(true, "default (not idle) keeps the update check gated");

        el.canUpdate = true;
        await el.updateComplete;
        expect(btn.disabled).to.equal(false, "idle enables the manual check");
    });

    it("emits a checkupdate event when the update button is pressed", async () => {
        const el = await fixture<AdvancedView>(html`<advanced-view .canUpdate=${true}></advanced-view>`);
        let fired = false;
        el.addEventListener("checkupdate", () => (fired = true));
        checkUpdateButton(el)!.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true }));
        expect(fired).to.equal(true);
    });
});
