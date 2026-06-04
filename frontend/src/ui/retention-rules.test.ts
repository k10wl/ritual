import { html } from "lit";
import { fixture, expect, oneEvent } from "@open-wc/testing";
import "./retention-rules";
import { describePolicy, type RetentionChangeDetail, type RetentionRulesEl } from "./retention-rules";
import type { RetentionRules } from "./retention-model";

const NOW = new Date("2026-06-04T12:00:00Z");

const r = (o: Partial<RetentionRules>): RetentionRules => ({
    keepLast: 0,
    keepDaily: 0,
    keepWeekly: 0,
    keepMonthly: 0,
    ...o,
});

const mount = (local: RetentionRules, remote: RetentionRules = r({ keepLast: 1 })) =>
    fixture<RetentionRulesEl>(
        html`<retention-rules .now=${NOW} .local=${local} .remote=${remote}></retention-rules>`,
    );

const steppers = (el: RetentionRulesEl) => [...el.shadowRoot!.querySelectorAll("rune-stepper")] as HTMLElement[];
const lane = (el: RetentionRulesEl, label: string) => el.shadowRoot!.querySelector(`.lane[data-tier="${label}"]`)!;
const dots = (el: RetentionRulesEl, label: string) => lane(el, label).querySelectorAll(".dot");
const setStepper = (s: HTMLElement, value: number) =>
    s.dispatchEvent(new CustomEvent("change", { detail: { value }, bubbles: true, composed: true }));

describe("describePolicy", () => {
    it("singular keep_last, multi-tier join", () => {
        expect(describePolicy(r({ keepLast: 1, keepWeekly: 5, keepMonthly: 1 }))).to.equal(
            "Keeps the most recent backup, 1 per week for 5 weeks, and 1 per month for 1 month.",
        );
    });
    it("plural keep_last only", () => {
        expect(describePolicy(r({ keepLast: 2 }))).to.equal("Keeps the 2 most recent backups.");
    });
    it("all zero", () => {
        expect(describePolicy(r({}))).to.equal("Nothing is kept — every backup is eventually removed.");
    });
});

describe("retention-rules", () => {
    it("renders a scope switch + four tier lanes, each with its own stepper", async () => {
        const el = await mount(r({ keepLast: 2 }));
        expect(el.shadowRoot!.querySelector("rune-segmented")).to.exist; // scope switch
        expect(steppers(el).length).to.equal(4); // one per lane, no separate block
        const labels = [...el.shadowRoot!.querySelectorAll(".lane-label")].map((l) => l.textContent!.trim());
        expect(labels).to.deep.equal(["Keep last", "Keep daily", "Keep weekly", "Keep monthly"]);
    });

    it("draws the calendar axis with month labels", async () => {
        const el = await mount(r({ keepMonthly: 3 }));
        const months = [...el.shadowRoot!.querySelectorAll(".axis .month")].map((m) => m.textContent!.trim());
        expect(months.length).to.be.greaterThan(1, "a multi-month reach must show month labels on the axis");
        expect(months[0]).to.match(/^[A-Z][a-z]{2}$/);
    });

    it("a lane shows one positioned dot per count; zero shows none", async () => {
        const el = await mount(r({ keepLast: 2, keepWeekly: 3 }));
        expect(dots(el, "Keep last").length).to.equal(2);
        expect(dots(el, "Keep weekly").length).to.equal(3);
        expect(dots(el, "Keep daily").length).to.equal(0);
        // dots are positioned along the shared axis
        expect((dots(el, "Keep weekly")[0] as HTMLElement).style.left).to.match(/%$/);
    });

    it("uncapped count: dots cap at 8 with a +N overflow, stepper has no max", async () => {
        const el = await mount(r({ keepDaily: 12 }));
        expect(dots(el, "Keep daily").length).to.equal(8);
        expect(lane(el, "Keep daily").querySelector(".more")!.textContent!.trim()).to.equal("+4");
        const daily = steppers(el)[1] as HTMLElement & { max: number };
        expect(Number.isFinite(daily.max)).to.equal(false);
    });

    it("editing a tier emits change {local, remote} and updates its dots live", async () => {
        const el = await mount(r({ keepLast: 2 }));
        setTimeout(() => setStepper(steppers(el)[0], 4));
        const ev = await oneEvent(el, "change");
        const d = ev.detail as RetentionChangeDetail;
        expect(d.local.keepLast).to.equal(4);
        expect(d.remote.keepLast).to.equal(1, "the untouched remote side carries through");
        await el.updateComplete;
        expect(dots(el, "Keep last").length).to.equal(4);
    });

    it("keep_last:0 shows the caution; non-zero hides it", async () => {
        const zero = await mount(r({ keepDaily: 1 }));
        expect(zero.shadowRoot!.querySelector(".caution")).to.exist;
        const nonzero = await mount(r({ keepLast: 1 }));
        expect(nonzero.shadowRoot!.querySelector(".caution")).to.not.exist;
    });

    it("scope switch edits the remote side independently", async () => {
        const el = await mount(r({ keepLast: 2 }), r({ keepLast: 3 }));
        el.shadowRoot!
            .querySelector("rune-segmented")!
            .dispatchEvent(new CustomEvent("change", { detail: { value: "remote" }, bubbles: true, composed: true }));
        await el.updateComplete;
        expect((steppers(el)[0] as HTMLElement & { value: number }).value).to.equal(3);

        setTimeout(() => setStepper(steppers(el)[0], 4));
        const ev = await oneEvent(el, "change");
        const d = ev.detail as RetentionChangeDetail;
        expect(d.remote.keepLast).to.equal(4);
        expect(d.local.keepLast).to.equal(2, "switching scope must not mutate the other side");
    });
});
