import { html } from "lit";
import { fixture, expect, oneEvent } from "@open-wc/testing";
import "./retention-rules";
import { describePolicy, explain, keptTotal, type RetentionChangeDetail, type RetentionRulesEl } from "./retention-rules";
import type { RetentionRules } from "./retention-model";

const r = (o: Partial<RetentionRules>): RetentionRules => ({
    keepLast: 0,
    keepDaily: 0,
    keepWeekly: 0,
    keepMonthly: 0,
    ...o,
});

const mount = (local: RetentionRules, remote: RetentionRules = r({ keepLast: 1 })) =>
    fixture<RetentionRulesEl>(html`<retention-rules .local=${local} .remote=${remote}></retention-rules>`);

const steppers = (el: RetentionRulesEl) => [...el.shadowRoot!.querySelectorAll("rune-stepper")] as HTMLElement[];
const rule = (el: RetentionRulesEl, label: string) => el.shadowRoot!.querySelector(`.rule[data-tier="${label}"]`)!;
// labels + explanations are rune-decoder elements; read the target via .text.
const decoderText = (node: Element | null) => (node as unknown as { text: string }).text;
const explainOf = (el: RetentionRulesEl, label: string) => decoderText(rule(el, label).querySelector(".explain"));
const setStepper = (s: HTMLElement, value: number) =>
    s.dispatchEvent(new CustomEvent("change", { detail: { value }, bubbles: true, composed: true }));

describe("explain", () => {
    it("zero reads as off", () => {
        expect(explain("daily", 0)).to.equal("Off — none kept this way.");
    });
    it("singular vs plural per tier", () => {
        expect(explain("last", 1)).to.equal("Keep your newest backup.");
        expect(explain("last", 9)).to.equal("Keep your 9 newest backups.");
        expect(explain("weekly", 1)).to.equal("Keep this week's backup.");
        expect(explain("weekly", 11)).to.equal("Keep one a week for 11 weeks.");
    });
});

describe("describePolicy", () => {
    it("singular keep_last, multi-tier join", () => {
        expect(describePolicy(r({ keepLast: 1, keepWeekly: 5, keepMonthly: 1 }))).to.equal(
            "Keeps the most recent backup, 1 per week for 5 weeks, and 1 per month for 1 month.",
        );
    });
    it("all zero", () => {
        expect(describePolicy(r({}))).to.equal("Nothing is kept — every backup is eventually removed.");
    });
});

describe("retention-rules", () => {
    it("renders a scope switch + four tier rules, each with its own stepper", async () => {
        const el = await mount(r({ keepLast: 2 }));
        expect(el.shadowRoot!.querySelector("rune-segmented")).to.exist; // scope switch
        expect(steppers(el).length).to.equal(4); // one per rule
        const labels = [...el.shadowRoot!.querySelectorAll(".rule-label")].map((l) => decoderText(l));
        expect(labels).to.deep.equal(["Keep last", "Keep daily", "Keep weekly", "Keep monthly"]);
    });

    it("each rule shows a plain-English sentence reflecting its count; zero dims + reads off", async () => {
        const el = await mount(r({ keepLast: 9, keepWeekly: 11 }));
        expect(explainOf(el, "Keep last")).to.equal("Keep your 9 newest backups.");
        expect(explainOf(el, "Keep weekly")).to.equal("Keep one a week for 11 weeks.");
        expect(explainOf(el, "Keep daily")).to.contain("Off");
        expect(rule(el, "Keep daily").hasAttribute("data-zero")).to.equal(true);
    });

    it("editing a tier emits change {local, remote} and rewrites its sentence live", async () => {
        const el = await mount(r({ keepLast: 1 }));
        expect(explainOf(el, "Keep daily")).to.contain("Off");
        setTimeout(() => setStepper(steppers(el)[1], 6)); // daily
        const ev = await oneEvent(el, "change");
        const d = ev.detail as RetentionChangeDetail;
        expect(d.local.keepDaily).to.equal(6);
        expect(d.remote.keepLast).to.equal(1, "the untouched remote side carries through");
        await el.updateComplete;
        expect(explainOf(el, "Keep daily")).to.equal("Keep one a day for 6 days.");
    });

    it("cascade nests one bar per active tier; no removed zone", async () => {
        const el = await mount(r({ keepLast: 2 }));
        expect(el.shadowRoot!.querySelectorAll(".nest .nest-row").length).to.equal(1);
        expect(el.shadowRoot!.querySelector(".gone")).to.not.exist;
    });

    it("nests every active tier (overlap is visible); subtle words + counts", async () => {
        const el = await mount(r({ keepLast: 10, keepDaily: 8, keepWeekly: 11 }));
        const labels = [...el.shadowRoot!.querySelectorAll(".nest .nest-label")].map((s) => decoderText(s));
        expect(labels).to.deep.equal(["last", "day", "week"]); // all active, none folded out
        const counts = [...el.shadowRoot!.querySelectorAll(".nest .bar-count")].map((s) => decoderText(s));
        expect(counts).to.deep.equal(["10", "8", "11"]);
        // coarser tier reaches further → its bar is wider (nested, left-anchored).
        const widths = [...el.shadowRoot!.querySelectorAll(".nest .bar")].map((b) =>
            parseFloat((b as HTMLElement).style.width),
        );
        expect(widths[2]).to.be.greaterThan(widths[0]); // week wider than last
    });

    it("cascade summary states the total kept, narrates the handoff + reach", async () => {
        const el = await mount(r({ keepWeekly: 11, keepMonthly: 3 }));
        const summary = decoderText(el.shadowRoot!.querySelector(".summary"));
        expect(summary).to.contain("Keeps about 11 backups"); // distinct total, not 14
        expect(summary).to.contain("then"); // handoff between tiers
        expect(summary).to.contain("3 months back"); // coarsest contributing tier
    });

    it("total accounts for overlap and warns when it's below the naive sum", async () => {
        // 1+1+1 all land on the newest backup → 1 distinct, not 3.
        expect(keptTotal(r({ keepLast: 1, keepWeekly: 1, keepMonthly: 1 }))).to.equal(1);
        const el = await mount(r({ keepLast: 1, keepWeekly: 1, keepMonthly: 1 }));
        const summary = decoderText(el.shadowRoot!.querySelector(".summary"));
        expect(summary).to.contain("Keeps about 1 backup");
        expect(summary).to.contain("Rules overlap");
    });

    it("all rules zero: no cascade band", async () => {
        const el = await mount(r({}));
        expect(el.shadowRoot!.querySelector(".band")).to.not.exist;
    });

    it("coerces missing tiers (backend omits zero fields) to 0 — no NaN/undefined", async () => {
        const partial = { keepLast: 4 } as unknown as RetentionRules; // daily/weekly/monthly undefined
        const el = await mount(partial);
        expect((steppers(el)[1] as HTMLElement & { value: number }).value).to.equal(0);
        expect(explainOf(el, "Keep daily")).to.contain("Off");
        const summary = decoderText(el.shadowRoot!.querySelector(".summary"));
        expect(summary).to.not.contain("undefined");
        expect(summary).to.not.contain("NaN");
        expect(summary).to.contain("Keeps about 4 backups");
    });

    it("uncapped tier: stepper has no max", async () => {
        const el = await mount(r({ keepDaily: 12 }));
        const daily = steppers(el)[1] as HTMLElement & { max: number };
        expect(Number.isFinite(daily.max)).to.equal(false);
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

    // ── design-log/045 §D — staged edits + Apply/Discard ─────────────────────

    it("no Apply bar when the picker is clean (baseline matches draft)", async () => {
        const el = await mount(r({ keepLast: 2 }));
        expect(el.shadowRoot!.querySelector(".apply-bar")).to.not.exist;
    });

    it("editing a tier reveals the Apply bar", async () => {
        const el = await mount(r({ keepLast: 2 }));
        setStepper(steppers(el)[0], 5);
        await el.updateComplete;
        expect(el.shadowRoot!.querySelector(".apply-bar"), "Apply bar after edit").to.exist;
    });

    it("editing back to the baseline clears the Apply bar (clean draft)", async () => {
        const el = await mount(r({ keepLast: 2 }));
        setStepper(steppers(el)[0], 5);
        await el.updateComplete;
        expect(el.shadowRoot!.querySelector(".apply-bar")).to.exist;
        setStepper(steppers(el)[0], 2); // back to baseline
        await el.updateComplete;
        expect(el.shadowRoot!.querySelector(".apply-bar")).to.not.exist;
    });

    it("Apply emits an apply event with {local, remote} and clears the bar", async () => {
        const el = await mount(r({ keepLast: 2 }), r({ keepLast: 3 }));
        setStepper(steppers(el)[0], 5); // local edit
        await el.updateComplete;
        const apply = el.shadowRoot!
            .querySelector(".apply-bar")!
            .querySelector("rune-button[variant='primary']") as HTMLElement;
        setTimeout(() =>
            apply.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true })),
        );
        const ev = await oneEvent(el, "apply");
        const d = ev.detail as RetentionChangeDetail;
        expect(d.local.keepLast).to.equal(5);
        expect(d.remote.keepLast).to.equal(3, "untouched side carries through");
        await el.updateComplete;
        expect(el.shadowRoot!.querySelector(".apply-bar")).to.not.exist;
    });

    it("Discard drops the staged edits back to baseline", async () => {
        const el = await mount(r({ keepLast: 2 }));
        setStepper(steppers(el)[0], 5);
        await el.updateComplete;
        const discard = el.shadowRoot!
            .querySelector(".apply-bar")!
            .querySelector("rune-button[variant='tinted']") as HTMLElement;
        discard.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true }));
        await el.updateComplete;
        expect(el.shadowRoot!.querySelector(".apply-bar")).to.not.exist;
        // The stepper now shows the baseline value again.
        expect((steppers(el)[0] as HTMLElement & { value: number }).value).to.equal(2);
    });

    it("stepper edits no longer auto-save: only `change` (not `apply`) fires on tap", async () => {
        const el = await mount(r({ keepLast: 2 }));
        let appliedFired = false;
        el.addEventListener("apply", () => (appliedFired = true));
        setStepper(steppers(el)[0], 5);
        await el.updateComplete;
        expect(appliedFired).to.equal(false, "apply must NOT fire on a stepper tap (staged edits)");
    });
});
